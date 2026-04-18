package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"
)

// newSummaryClassifiedMR is a test helper that builds a ClassifiedMR with a
// configurable author, creation time, and status.
func newSummaryClassifiedMR(title, authorName, url string, createdAt time.Time, status MRStatus) *ClassifiedMR {
	return &ClassifiedMR{
		MR: &MergeRequestWithApprovals{
			MergeRequest: &gitlab.MergeRequest{
				Title:     title,
				WebURL:    url,
				Author:    &gitlab.BasicUser{Name: authorName, Username: strings.ToLower(authorName)},
				CreatedAt: timePtr(createdAt),
			},
		},
		Status: status,
	}
}

func TestFormatSummaryMessages_Empty(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	messages := formatSummaryMessages(nil, 7, now)

	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], ":clipboard:")
	assert.Contains(t, messages[0], "2026-04-18 09:00")
	assert.Contains(t, messages[0], "열린 MR: 총 0개")
}

func TestFormatSummaryMessages_SingleStatus(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	classified := []*ClassifiedMR{
		newSummaryClassifiedMR("Fix crash", "Alice", "https://example/1",
			now.Add(-2*24*time.Hour), StatusNeedsReview),
		newSummaryClassifiedMR("Add feature", "Bob", "https://example/2",
			now.Add(-1*24*time.Hour), StatusNeedsReview),
	}

	messages := formatSummaryMessages(classified, 7, now)

	require.Len(t, messages, 1)
	msg := messages[0]
	assert.Contains(t, msg, "열린 MR: 총 2개")
	assert.Contains(t, msg, ":eyes: *리뷰 대기* (2)")
	assert.Contains(t, msg, "<https://example/1|Fix crash>")
	assert.Contains(t, msg, "<https://example/2|Add feature>")
	// No other status sections present.
	assert.NotContains(t, msg, ":warning: *충돌")
	assert.NotContains(t, msg, ":no_entry_sign:")
}

func TestFormatSummaryMessages_AllStatuses(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * 24 * time.Hour)

	classified := []*ClassifiedMR{
		newSummaryClassifiedMR("Needs review 1", "Alice", "https://example/nr",
			recent, StatusNeedsReview),
		newSummaryClassifiedMR("Approved 1", "Bob", "https://example/ap",
			recent, StatusApprovedPendingMerge),
		newSummaryClassifiedMR("Changes 1", "Carol", "https://example/cr",
			recent, StatusChangesRequested),
		newSummaryClassifiedMR("Blocking 1", "Dave", "https://example/bd",
			recent, StatusBlockingDiscussions),
		newSummaryClassifiedMR("Conflict 1", "Eve", "https://example/hc",
			recent, StatusHasConflicts),
	}

	messages := formatSummaryMessages(classified, 7, now)
	require.Len(t, messages, 1)
	msg := messages[0]

	// Urgency order: has_conflicts > blocking > changes_requested > approved > needs_review
	idxConflict := strings.Index(msg, ":warning: *충돌 해결 필요*")
	idxBlocking := strings.Index(msg, ":no_entry_sign: *미해결 블로킹 디스커션*")
	idxChanges := strings.Index(msg, ":pencil2: *변경 요청됨*")
	idxApproved := strings.Index(msg, ":white_check_mark: *승인됨 — 머지 대기*")
	idxReview := strings.Index(msg, ":eyes: *리뷰 대기*")

	require.NotEqual(t, -1, idxConflict)
	require.NotEqual(t, -1, idxBlocking)
	require.NotEqual(t, -1, idxChanges)
	require.NotEqual(t, -1, idxApproved)
	require.NotEqual(t, -1, idxReview)

	assert.Less(t, idxConflict, idxBlocking)
	assert.Less(t, idxBlocking, idxChanges)
	assert.Less(t, idxChanges, idxApproved)
	assert.Less(t, idxApproved, idxReview)
}

func TestFormatSummaryMessages_EmptyGroups_Skipped(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	classified := []*ClassifiedMR{
		newSummaryClassifiedMR("Only conflict", "Alice", "https://example/1",
			now.Add(-24*time.Hour), StatusHasConflicts),
	}

	messages := formatSummaryMessages(classified, 7, now)
	require.Len(t, messages, 1)
	msg := messages[0]

	assert.Contains(t, msg, ":warning: *충돌 해결 필요* (1)")
	assert.NotContains(t, msg, ":no_entry_sign:")
	assert.NotContains(t, msg, ":pencil2:")
	assert.NotContains(t, msg, ":white_check_mark:")
	assert.NotContains(t, msg, ":eyes: *리뷰 대기*")
}

func TestFormatSummaryMessages_StaleHighlight(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	classified := []*ClassifiedMR{
		// Fresh — 3일 전, not stale
		newSummaryClassifiedMR("Fresh MR", "Alice", "https://example/fresh",
			now.Add(-3*24*time.Hour), StatusNeedsReview),
		// Stale — 10일 전
		newSummaryClassifiedMR("Old MR", "Bob", "https://example/old",
			now.Add(-10*24*time.Hour), StatusNeedsReview),
	}

	messages := formatSummaryMessages(classified, 7, now)
	require.Len(t, messages, 1)
	msg := messages[0]

	lines := strings.Split(msg, "\n")
	var freshLine, oldLine string
	for _, line := range lines {
		if strings.Contains(line, "Fresh MR") {
			freshLine = line
		}
		if strings.Contains(line, "Old MR") {
			oldLine = line
		}
	}

	require.NotEmpty(t, freshLine)
	require.NotEmpty(t, oldLine)
	assert.NotContains(t, freshLine, ":exclamation:")
	assert.Contains(t, oldLine, ":exclamation:")
}

func TestFormatSummaryMessages_SplitByCount(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	createdAt := now.Add(-24 * time.Hour)

	// 45 MRs all needs_review — forces split at 40.
	classified := make([]*ClassifiedMR, 0, 45)
	for i := 0; i < 45; i++ {
		classified = append(classified, newSummaryClassifiedMR(
			"MR", "Author",
			"https://example/"+strings.Repeat("x", 1),
			createdAt, StatusNeedsReview,
		))
	}

	messages := formatSummaryMessages(classified, 7, now)
	require.GreaterOrEqual(t, len(messages), 2, "45 MRs should split into at least 2 messages")

	// Every message retains the summary header.
	for _, m := range messages {
		assert.Contains(t, m, ":clipboard: *MR 현황 요약*")
		assert.Contains(t, m, "열린 MR: 총 45개")
	}
}

func TestGroupClassifiedByStatus(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	classified := []*ClassifiedMR{
		newSummaryClassifiedMR("A", "Alice", "https://example/a", now, StatusNeedsReview),
		newSummaryClassifiedMR("B", "Bob", "https://example/b", now, StatusNeedsReview),
		newSummaryClassifiedMR("C", "Carol", "https://example/c", now, StatusHasConflicts),
	}

	grouped := groupClassifiedByStatus(classified)

	assert.Len(t, grouped[StatusNeedsReview], 2)
	assert.Len(t, grouped[StatusHasConflicts], 1)
	assert.Len(t, grouped[StatusApprovedPendingMerge], 0)
}

func TestFormatRelativeTime_Days(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	past := now.Add(-3 * 24 * time.Hour)
	assert.Equal(t, "3일 전", formatRelativeTime(past, now))
}

func TestFormatRelativeTime_Hours(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	past := now.Add(-5 * time.Hour)
	assert.Equal(t, "5시간 전", formatRelativeTime(past, now))
}

func TestFormatRelativeTime_JustNow(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute)
	assert.Equal(t, "방금 전", formatRelativeTime(past, now))
}

func TestIsStale_True(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	mr := newSummaryClassifiedMR("Old", "Alice", "https://example/o",
		now.Add(-10*24*time.Hour), StatusNeedsReview)
	assert.True(t, isStale(mr, 7, now))
}

func TestIsStale_False(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	mr := newSummaryClassifiedMR("Fresh", "Alice", "https://example/f",
		now.Add(-3*24*time.Hour), StatusNeedsReview)
	assert.False(t, isStale(mr, 7, now))
}

func TestResolveSummaryWebhookURL_Explicit(t *testing.T) {
	config := &Config{
		Summary: &SummaryConfig{WebhookURL: "https://hooks/summary"},
		Notification: &NotificationConfig{
			Webhook: &WebhookConfig{URL: "https://hooks/notification"},
		},
	}
	assert.Equal(t, "https://hooks/summary", resolveSummaryWebhookURL(config))
}

func TestResolveSummaryWebhookURL_Fallback(t *testing.T) {
	config := &Config{
		Summary: &SummaryConfig{},
		Notification: &NotificationConfig{
			Webhook: &WebhookConfig{URL: "https://hooks/notification"},
		},
	}
	assert.Equal(t, "https://hooks/notification", resolveSummaryWebhookURL(config))
}

func TestResolveSummaryWebhookURL_Empty(t *testing.T) {
	config := &Config{}
	assert.Equal(t, "", resolveSummaryWebhookURL(config))
}

func TestResolveStaleDays_Explicit(t *testing.T) {
	config := &Config{Summary: &SummaryConfig{StaleDays: 14}}
	assert.Equal(t, 14, resolveStaleDays(config))
}

func TestResolveStaleDays_Default(t *testing.T) {
	config := &Config{}
	assert.Equal(t, 7, resolveStaleDays(config))

	config2 := &Config{Summary: &SummaryConfig{}}
	assert.Equal(t, 7, resolveStaleDays(config2))
}
