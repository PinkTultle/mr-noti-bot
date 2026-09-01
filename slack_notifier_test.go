package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/flexoid/mergentle-reminder/mocks"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"
)

// --- helpers ---

// newTestNotification creates a Notification for testing with the
// given parameters.
func newTestNotification(
	title, url, authorName string,
	status MRStatus,
	targets []NotificationTarget,
) *Notification {
	cmr := &ClassifiedMR{
		MR: &MergeRequestWithApprovals{
			MergeRequest: &gitlab.MergeRequest{
				Title:  title,
				WebURL: url,
				Author: &gitlab.BasicUser{Name: authorName},
			},
		},
		Status: status,
	}
	return &Notification{
		MR:      cmr,
		Targets: targets,
		Message: fmt.Sprintf("%s\n<%s|%s> (by %s)", statusMessages[status], url, title, authorName),
	}
}

// --- WebhookNotifier tests ---

func TestWebhookNotifier_Send(t *testing.T) {
	n := newTestNotification(
		"Fix bug", "https://gitlab.com/mr/1", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U123", GitLabUsername: "bob", Role: RoleReviewer},
			{SlackID: "U456", GitLabUsername: "carol", Role: RoleReviewer},
		},
	)

	// We cannot easily mock slack.PostWebhook (package-level function),
	// so we verify the text building logic via grouping and format tests.
	// This test verifies the text content that would be posted.
	grouped := groupNotificationsByStatus([]*Notification{n})

	assert.Len(t, grouped, 1)
	assert.Contains(t, grouped, StatusNeedsReview)

	notifs := grouped[StatusNeedsReview]
	require.Len(t, notifs, 1)
	assert.Equal(t, "Fix bug", notifs[0].MR.MR.MergeRequest.Title)
	assert.Len(t, notifs[0].Targets, 2)

	// Verify mention format
	mentions := formatMentions(notifs[0].Targets)
	assert.Contains(t, mentions, "<@U123>")
	assert.Contains(t, mentions, "<@U456>")
}

func TestWebhookNotifier_EmptyTargets(t *testing.T) {
	// Notification where Targets is empty — MR info shown but no mention line.
	// When user mapping is absent or no mapping matches, Targets will be empty.
	n := newTestNotification(
		"Unmapped MR", "https://gitlab.com/mr/10", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{}, // empty — no Slack mapping
	)

	grouped := groupNotificationsByStatus([]*Notification{n})

	require.Len(t, grouped, 1)
	require.Contains(t, grouped, StatusNeedsReview)

	notifs := grouped[StatusNeedsReview]
	require.Len(t, notifs, 1)
	assert.Equal(t, "Unmapped MR", notifs[0].MR.MR.MergeRequest.Title)
	assert.Empty(t, notifs[0].Targets)

	// formatMentions with empty targets should produce an empty string
	mentions := formatMentions(notifs[0].Targets)
	assert.Equal(t, "", mentions)
}

func TestWebhookNotifier_EmptyNotifications(t *testing.T) {
	w := &WebhookNotifier{webhookURL: "https://hooks.slack.com/test"}
	err := w.Send([]*Notification{})
	assert.NoError(t, err)
}

func TestWebhookNotifier_Send_ActuallyPosts(t *testing.T) {
	mockClient := mocks.NewSlackClient(t)
	var captured []string
	mockClient.On("PostWebhook", mock.Anything).Run(func(args mock.Arguments) {
		msg := args.Get(0).(*slack.WebhookMessage)
		captured = append(captured, msg.Text)
	}).Return(nil)

	n := &WebhookNotifier{webhookURL: "https://example.invalid", client: mockClient}
	err := n.Send([]*Notification{
		newSingleTargetNotification(StatusNeedsReview, "MR A", "https://gl/1", "alice", "U1"),
	})

	require.NoError(t, err)
	require.Len(t, captured, 1)
	assert.Contains(t, captured[0], "MR A")
	assert.Contains(t, captured[0], "<@U1>")
	mockClient.AssertExpectations(t)
}

func TestWebhookNotifier_Send_ChunksLargePayload(t *testing.T) {
	var notifications []*Notification
	for i := 0; i < 95; i++ { // 40 * 2 + 15 → 3개 메시지
		notifications = append(notifications, newSingleTargetNotification(
			StatusNeedsReview,
			fmt.Sprintf("MR %d", i),
			fmt.Sprintf("https://gl/%d", i),
			"alice", "U1"))
	}

	mockClient := mocks.NewSlackClient(t)
	var sent []string
	mockClient.On("PostWebhook", mock.Anything).Run(func(args mock.Arguments) {
		sent = append(sent, args.Get(0).(*slack.WebhookMessage).Text)
	}).Return(nil)

	n := &WebhookNotifier{webhookURL: "https://example.invalid", client: mockClient}
	require.NoError(t, n.Send(notifications))

	assert.Len(t, sent, 3, "MR 95개는 40개 단위로 3개 메시지로 나뉘어야 한다")
	for i, msg := range sent {
		assert.Less(t, len(msg), 40000, "메시지 %d가 Slack 한도를 넘었다", i)
	}
	// 첫 MR과 마지막 MR이 모두 전송되어야 한다 (유실 없음)
	all := strings.Join(sent, "")
	assert.Contains(t, all, "MR 0")
	assert.Contains(t, all, "MR 94")
}

func TestWebhookNotifier_Send_StopsOnFirstError(t *testing.T) {
	var notifications []*Notification
	for i := 0; i < 50; i++ {
		notifications = append(notifications, newSingleTargetNotification(
			StatusNeedsReview, fmt.Sprintf("MR %d", i), "https://gl/x", "alice", "U1"))
	}

	mockClient := mocks.NewSlackClient(t)
	mockClient.On("PostWebhook", mock.Anything).Return(errors.New("slack down")).Once()

	n := &WebhookNotifier{webhookURL: "https://example.invalid", client: mockClient}
	err := n.Send(notifications)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slack down")
}

// 상태 A가 정확히 maxMRsPerMessage(40)개로 끝나고 이어서 상태 B가 등장할 때,
// B의 헤더가 항목 없이 A의 메시지 끝에 딸려 나가지 않는지 검증한다.
// (리뷰에서 재현된 회귀: HasConflicts 40개 + BlockingDiscussions 1개)
func TestBuildWebhookMessages_NoDanglingHeaderAtChunkBoundary(t *testing.T) {
	var notifications []*Notification
	for i := 0; i < maxMRsPerMessage; i++ {
		notifications = append(notifications, newSingleTargetNotification(
			StatusHasConflicts,
			fmt.Sprintf("Conflict MR %d", i),
			fmt.Sprintf("https://gl/c/%d", i),
			"alice", "U1"))
	}
	notifications = append(notifications, newSingleTargetNotification(
		StatusBlockingDiscussions, "Blocking MR", "https://gl/b/0", "bob", "U2"))

	messages := buildWebhookMessages(notifications)
	require.Len(t, messages, 2, "상태 A가 정확히 40개로 끝나면 상태 B는 새 메시지로 넘어가야 한다")

	conflictHeader := statusMessages[StatusHasConflicts]
	blockingHeader := statusMessages[StatusBlockingDiscussions]

	// (a) 헤더 없이 항목만 있는 메시지가 없다 — 모든 메시지가 알려진 상태 헤더로 시작한다
	for i, msg := range messages {
		assert.True(t,
			strings.HasPrefix(msg, conflictHeader) || strings.HasPrefix(msg, blockingHeader),
			"메시지 %d가 알려진 상태 헤더로 시작하지 않는다: %q", i, msg)
	}

	// 첫 메시지에는 다음 상태(B)의 헤더가 전혀 등장하면 안 된다 — 등장한다면
	// 항목 없이 헤더만 딸려 나간 회귀가 재발한 것이다
	assert.NotContains(t, messages[0], blockingHeader)

	// (b) 헤더가 항목 없이 등장하는 메시지가 없다 — B의 헤더 뒤에는 반드시
	// 실제 MR 항목(제목)이 뒤따른다
	require.Contains(t, messages[1], blockingHeader)
	idx := strings.Index(messages[1], blockingHeader)
	rest := messages[1][idx+len(blockingHeader):]
	assert.Contains(t, rest, "Blocking MR", "블로킹 헤더 뒤에 실제 항목이 없다 — 항목 없는 헤더만 남는 회귀")

	// (c) B의 헤더는 새 메시지에서 정확히 한 번만 나타난다
	assert.Equal(t, 1, strings.Count(messages[1], blockingHeader))
}

// newSingleTargetNotification은 단일 대상 알림을 조립하는 헬퍼다.
// 기존 newTestNotification과 달리 ProjectID/IID를 채운다.
func newSingleTargetNotification(status MRStatus, title, url, author, slackID string) *Notification {
	return &Notification{
		MR: &ClassifiedMR{
			Status: status,
			MR: &MergeRequestWithApprovals{
				ProjectID: 1,
				MergeRequest: &gitlab.MergeRequest{
					IID: 1, Title: title, WebURL: url,
					Author: &gitlab.BasicUser{Username: author, Name: author},
				},
			},
		},
		Targets: []NotificationTarget{{SlackID: slackID, GitLabUsername: author, Role: RoleAuthor}},
	}
}

// --- DMNotifier tests ---

// mockSlackAPI implements SlackAPI for testing.
type mockSlackAPI struct {
	mock.Mock
}

func (m *mockSlackAPI) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	args := m.Called(channelID, options)
	return args.String(0), args.String(1), args.Error(2)
}

func TestDMNotifier_Send(t *testing.T) {
	mockAPI := new(mockSlackAPI)

	notifier := &DMNotifier{
		token:  "xoxb-test-token",
		client: mockAPI,
	}

	n := newTestNotification(
		"Add feature", "https://gitlab.com/mr/2", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
		},
	)

	// Expect PostMessage to U100
	mockAPI.On("PostMessage", "U100", mock.Anything).
		Return("", "", nil).Once()

	err := notifier.Send([]*Notification{n})
	assert.NoError(t, err)
	mockAPI.AssertExpectations(t)
}

func TestDMNotifier_MultipleMRsSameTarget(t *testing.T) {
	mockAPI := new(mockSlackAPI)

	notifier := &DMNotifier{
		token:  "xoxb-test-token",
		client: mockAPI,
	}

	n1 := newTestNotification(
		"MR One", "https://gitlab.com/mr/1", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
		},
	)
	n2 := newTestNotification(
		"MR Two", "https://gitlab.com/mr/2", "Carol",
		StatusHasConflicts,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
		},
	)

	// Same target U100 should receive one PostMessage call
	mockAPI.On("PostMessage", "U100", mock.Anything).
		Return("", "", nil).Once()

	err := notifier.Send([]*Notification{n1, n2})
	assert.NoError(t, err)
	mockAPI.AssertExpectations(t)
}

func TestDMNotifier_APIError(t *testing.T) {
	mockAPI := new(mockSlackAPI)

	notifier := &DMNotifier{
		token:  "xoxb-test-token",
		client: mockAPI,
	}

	n1 := newTestNotification(
		"MR One", "https://gitlab.com/mr/1", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
		},
	)
	n2 := newTestNotification(
		"MR Two", "https://gitlab.com/mr/2", "Carol",
		StatusApprovedPendingMerge,
		[]NotificationTarget{
			{SlackID: "U200", GitLabUsername: "dave", Role: RoleAuthor},
		},
	)

	// First target fails, second succeeds
	mockAPI.On("PostMessage", "U100", mock.Anything).
		Return("", "", fmt.Errorf("channel_not_found")).Once()
	mockAPI.On("PostMessage", "U200", mock.Anything).
		Return("", "", nil).Once()

	err := notifier.Send([]*Notification{n1, n2})
	// 실패한 대상이 있으면 반드시 error를 반환해야 한다 — 그래야 호출부가
	// saveState를 건너뛰고, 실패한 알림이 다음 사이클에 재시도된다.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "U100")
	mockAPI.AssertExpectations(t)
}

func TestDMNotifier_MultipleDistinctTargets(t *testing.T) {
	// 3 different target SlackIDs → PostMessage called 3 times, one per user.
	mockAPI := new(mockSlackAPI)

	notifier := &DMNotifier{
		token:  "xoxb-test-token",
		client: mockAPI,
	}

	n1 := newTestNotification(
		"MR Alpha", "https://gitlab.com/mr/10", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
		},
	)
	n2 := newTestNotification(
		"MR Beta", "https://gitlab.com/mr/11", "Alice",
		StatusHasConflicts,
		[]NotificationTarget{
			{SlackID: "U200", GitLabUsername: "carol", Role: RoleAuthor},
		},
	)
	n3 := newTestNotification(
		"MR Gamma", "https://gitlab.com/mr/12", "Dave",
		StatusApprovedPendingMerge,
		[]NotificationTarget{
			{SlackID: "U300", GitLabUsername: "eve", Role: RoleAuthor},
		},
	)

	// Each distinct target should receive exactly one PostMessage call
	mockAPI.On("PostMessage", "U100", mock.Anything).
		Return("", "", nil).Once()
	mockAPI.On("PostMessage", "U200", mock.Anything).
		Return("", "", nil).Once()
	mockAPI.On("PostMessage", "U300", mock.Anything).
		Return("", "", nil).Once()

	err := notifier.Send([]*Notification{n1, n2, n3})
	assert.NoError(t, err)
	mockAPI.AssertExpectations(t)
}

func TestDMNotifier_EmptyNotifications(t *testing.T) {
	notifier := &DMNotifier{token: "xoxb-test-token"}
	err := notifier.Send([]*Notification{})
	assert.NoError(t, err)
}

func TestDMNotifier_Send_ReturnsErrorOnPartialFailure(t *testing.T) {
	mockAPI := mocks.NewSlackAPI(t)
	mockAPI.On("PostMessage", "U_OK", mock.Anything).Return("", "", nil)
	mockAPI.On("PostMessage", "U_BAD", mock.Anything).
		Return("", "", errors.New("channel_not_found"))

	n := &DMNotifier{token: "xoxb-test", client: mockAPI}
	err := n.Send([]*Notification{
		newSingleTargetNotification(StatusNeedsReview, "MR A", "https://gl/1", "alice", "U_OK"),
		newSingleTargetNotification(StatusNeedsReview, "MR B", "https://gl/2", "bob", "U_BAD"),
	})

	require.Error(t, err, "DM 전송 실패는 반드시 호출부에 전달되어야 한다")
	assert.Contains(t, err.Error(), "1")
}

func TestDMNotifier_Send_NilOnAllSuccess(t *testing.T) {
	mockAPI := mocks.NewSlackAPI(t)
	mockAPI.On("PostMessage", mock.Anything, mock.Anything).Return("", "", nil)

	n := &DMNotifier{token: "xoxb-test", client: mockAPI}
	err := n.Send([]*Notification{
		newSingleTargetNotification(StatusNeedsReview, "MR A", "https://gl/1", "alice", "U1"),
	})
	require.NoError(t, err)
}

// --- LegacyNotifier tests ---

func TestLegacyNotifier_Send(t *testing.T) {
	mockClient := mocks.NewSlackClient(t)

	notifier := &LegacyNotifier{client: mockClient}

	n := newTestNotification(
		"Fix bug", "https://gitlab.com/mr/1", "Alice",
		StatusNeedsReview,
		nil,
	)

	expectedText := n.Message + "\n\n"
	mockClient.EXPECT().PostWebhook(&slack.WebhookMessage{Text: expectedText}).Return(nil)

	err := notifier.Send([]*Notification{n})
	assert.NoError(t, err)
}

func TestLegacyNotifier_EmptyNotifications(t *testing.T) {
	mockClient := mocks.NewSlackClient(t)
	notifier := &LegacyNotifier{client: mockClient}
	err := notifier.Send([]*Notification{})
	assert.NoError(t, err)
}

// --- newNotifier factory tests ---

func TestNewNotifier_Legacy(t *testing.T) {
	config := &Config{
		Notification: nil,
	}
	config.Slack.WebhookURL = "https://hooks.slack.com/legacy"

	notifier, err := newNotifier(config)
	require.NoError(t, err)

	legacy, ok := notifier.(*LegacyNotifier)
	require.True(t, ok, "expected *LegacyNotifier")
	assert.NotNil(t, legacy.client)
}

func TestNewNotifier_Webhook(t *testing.T) {
	config := &Config{
		Notification: &NotificationConfig{
			Mode:    "webhook",
			Webhook: &WebhookConfig{URL: "https://hooks.slack.com/new"},
		},
	}

	notifier, err := newNotifier(config)
	require.NoError(t, err)

	wn, ok := notifier.(*WebhookNotifier)
	require.True(t, ok, "expected *WebhookNotifier")
	assert.Equal(t, "https://hooks.slack.com/new", wn.webhookURL)
}

func TestNewNotifier_DM(t *testing.T) {
	config := &Config{
		Notification: &NotificationConfig{
			Mode: "dm",
			Bot:  &BotConfig{Token: "xoxb-bot-token"},
		},
	}

	notifier, err := newNotifier(config)
	require.NoError(t, err)

	dm, ok := notifier.(*DMNotifier)
	require.True(t, ok, "expected *DMNotifier")
	assert.Equal(t, "xoxb-bot-token", dm.token)
	assert.Nil(t, dm.client, "client should be nil until first Send")
}

func TestNewNotifier_DefaultMode(t *testing.T) {
	// When mode is empty or unrecognized, default to WebhookNotifier
	config := &Config{
		Notification: &NotificationConfig{
			Mode:    "",
			Webhook: &WebhookConfig{URL: "https://hooks.slack.com/default"},
		},
	}

	notifier, err := newNotifier(config)
	require.NoError(t, err)

	_, ok := notifier.(*WebhookNotifier)
	assert.True(t, ok, "expected *WebhookNotifier for default/empty mode")
}

func TestNewNotifier_DMModeWithoutBotConfig(t *testing.T) {
	config := &Config{Notification: &NotificationConfig{Mode: "dm"}} // Bot이 nil
	_, err := newNotifier(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot")
}

func TestNewNotifier_WebhookModeWithoutWebhookConfig(t *testing.T) {
	config := &Config{Notification: &NotificationConfig{Mode: "webhook"}} // Webhook이 nil
	_, err := newNotifier(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook")
}

// --- groupNotificationsByStatus tests ---

func TestGroupNotificationsByStatus(t *testing.T) {
	n1 := newTestNotification("MR1", "https://gitlab.com/mr/1", "Alice", StatusNeedsReview, nil)
	n2 := newTestNotification("MR2", "https://gitlab.com/mr/2", "Bob", StatusNeedsReview, nil)
	n3 := newTestNotification("MR3", "https://gitlab.com/mr/3", "Carol", StatusHasConflicts, nil)
	n4 := newTestNotification("MR4", "https://gitlab.com/mr/4", "Dave", StatusApprovedPendingMerge, nil)

	grouped := groupNotificationsByStatus([]*Notification{n1, n2, n3, n4})

	assert.Len(t, grouped, 3)
	assert.Len(t, grouped[StatusNeedsReview], 2)
	assert.Len(t, grouped[StatusHasConflicts], 1)
	assert.Len(t, grouped[StatusApprovedPendingMerge], 1)
}

func TestGroupNotificationsByStatus_Empty(t *testing.T) {
	grouped := groupNotificationsByStatus([]*Notification{})
	assert.Empty(t, grouped)
}

// --- groupNotificationsByTarget tests ---

func TestGroupNotificationsByTarget(t *testing.T) {
	n1 := newTestNotification(
		"MR1", "https://gitlab.com/mr/1", "Alice",
		StatusNeedsReview,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleReviewer},
			{SlackID: "U200", GitLabUsername: "carol", Role: RoleReviewer},
		},
	)
	n2 := newTestNotification(
		"MR2", "https://gitlab.com/mr/2", "Dave",
		StatusHasConflicts,
		[]NotificationTarget{
			{SlackID: "U100", GitLabUsername: "bob", Role: RoleAuthor},
		},
	)

	grouped := groupNotificationsByTarget([]*Notification{n1, n2})

	assert.Len(t, grouped, 2)

	// U100 should have 2 notifications (from n1 and n2)
	assert.Len(t, grouped["U100"], 2)
	assert.Equal(t, "MR1", grouped["U100"][0].MR.MR.MergeRequest.Title)
	assert.Equal(t, "MR2", grouped["U100"][1].MR.MR.MergeRequest.Title)

	// U200 should have 1 notification (from n1 only)
	assert.Len(t, grouped["U200"], 1)
	assert.Equal(t, "MR1", grouped["U200"][0].MR.MR.MergeRequest.Title)
}

func TestGroupNotificationsByTarget_NoTargets(t *testing.T) {
	n := newTestNotification("MR1", "https://gitlab.com/mr/1", "Alice", StatusNeedsReview, nil)

	grouped := groupNotificationsByTarget([]*Notification{n})
	assert.Empty(t, grouped)
}

func TestGroupNotificationsByTarget_Empty(t *testing.T) {
	grouped := groupNotificationsByTarget([]*Notification{})
	assert.Empty(t, grouped)
}
