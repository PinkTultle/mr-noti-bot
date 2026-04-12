package main

import (
	"fmt"
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
	// Should not return error (errors are logged, sending continues)
	assert.NoError(t, err)
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

	notifier := newNotifier(config)

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

	notifier := newNotifier(config)

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

	notifier := newNotifier(config)

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

	notifier := newNotifier(config)

	_, ok := notifier.(*WebhookNotifier)
	assert.True(t, ok, "expected *WebhookNotifier for default/empty mode")
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
