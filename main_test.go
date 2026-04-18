package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flexoid/mergentle-reminder/mocks"
	slack "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"
)

func TestFilterMergeRequestsByAuthor(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 1, Name: "John Doe", Username: "johndoe"},
			},
		},
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 2, Name: "James Doe", Username: "jamesdoe"},
			},
		},
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 2, Name: "Jane Doe", Username: "janedoe"},
			},
		},
	}

	authors := []ConfigAuthor{
		{ID: 1},
		{Username: "janedoe"},
	}

	filteredMRs := filterMergeRequestsByAuthor(mrs, authors)

	require.Equal(t, 2, len(filteredMRs))
	assert.Equal(t, 1, filteredMRs[0].MergeRequest.Author.ID)
	assert.Equal(t, 2, filteredMRs[1].MergeRequest.Author.ID)
}

func TestFilterMergeRequestsByAuthor_NoMatchingAuthors(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 3, Name: "Alice", Username: "alice"},
			},
		},
	}
	authors := []ConfigAuthor{
		{ID: 1},
		{Username: "janedoe"},
	}
	filteredMRs := filterMergeRequestsByAuthor(mrs, authors)
	require.Equal(t, 0, len(filteredMRs))
}

func TestFilterMergeRequestsByAuthor_MultipleMergeRequestsSameAuthor(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 1, Name: "John Doe", Username: "johndoe"},
			},
		},
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 1, Name: "John Doe", Username: "johndoe"},
			},
		},
	}
	authors := []ConfigAuthor{
		{ID: 1},
	}
	filteredMRs := filterMergeRequestsByAuthor(mrs, authors)
	require.Equal(t, 2, len(filteredMRs))
	assert.Equal(t, 1, filteredMRs[0].MergeRequest.Author.ID)
	assert.Equal(t, 1, filteredMRs[1].MergeRequest.Author.ID)
}

func TestFilterMergeRequestsByAuthor_EmptyMergeRequests(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{}
	authors := []ConfigAuthor{
		{ID: 1},
	}
	filteredMRs := filterMergeRequestsByAuthor(mrs, authors)
	require.Equal(t, 0, len(filteredMRs))
}

func TestFilterMergeRequestsByAuthor_OptionalAuthors(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: &gitlab.BasicUser{ID: 1, Name: "John Doe", Username: "johndoe"},
			},
		},
	}
	authors := []ConfigAuthor{}
	filteredMRs := filterMergeRequestsByAuthor(mrs, authors)
	require.Equal(t, 1, len(filteredMRs))
}

// --- Integration Tests ---

// TestExecuteLegacy verifies the legacy execution path: filter by author,
// format summary, and send via SlackClient mock. Since executeLegacy creates
// its own slackClient internally, we test the pipeline components together.
func TestExecuteLegacy(t *testing.T) {
	// Setup: 1 MR with author matching the Authors filter
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Title:                      "Fix login bug",
				WebURL:                     "https://gitlab.com/test/project/-/merge_requests/42",
				Author:                     &gitlab.BasicUser{ID: 10, Name: "Alice", Username: "alice"},
				CreatedAt:                  timePtr(time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)),
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  1,
		},
	}

	authors := []ConfigAuthor{
		{Username: "alice"},
	}

	// Step 1: Filter by author
	filtered := filterMergeRequestsByAuthor(mrs, authors)
	require.Len(t, filtered, 1, "MR from alice should pass the author filter")

	// Step 2: Format summary
	summary := formatMergeRequestsSummary(filtered)
	assert.Contains(t, summary, "Fix login bug")
	assert.Contains(t, summary, "Alice")
	assert.Contains(t, summary, "https://gitlab.com/test/project/-/merge_requests/42")

	// Step 3: Send via mock SlackClient
	mockClient := mocks.NewSlackClient(t)
	mockClient.EXPECT().PostWebhook(mock.MatchedBy(func(msg *slack.WebhookMessage) bool {
		return msg.Text == summary
	})).Return(nil)

	err := sendSlackMessage(mockClient, summary)
	require.NoError(t, err)
}

// TestExecuteSmartNotification_WebhookMode tests the smart notification pipeline
// in webhook mode: classify -> filter (new MR) -> resolve targets -> verify state.
func TestExecuteSmartNotification_WebhookMode(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// 1 MR: no approvals, discussions resolved → needs_review
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                        101,
				Title:                      "Add dark mode",
				WebURL:                     "https://gitlab.com/test/project/-/merge_requests/101",
				Author:                     &gitlab.BasicUser{ID: 20, Name: "Bob", Username: "bob"},
				HasConflicts:               false,
				BlockingDiscussionsResolved: true,
				Reviewers:                  []*gitlab.BasicUser{{Username: "carol", Name: "Carol"}},
			},
			ApprovedBy: []string{},
			ProjectID:  5,
		},
	}

	mapping := []UserMappingEntry{
		{GitLabUsername: "bob", SlackID: "U200"},
		{GitLabUsername: "carol", SlackID: "U300"},
	}

	// Step 1: Classify
	classified := classifyMergeRequests(mrs)
	require.Len(t, classified, 1)
	assert.Equal(t, StatusNeedsReview, classified[0].Status)

	// Step 2: Load state (no file yet → empty state)
	prevState, err := loadState(statePath)
	require.NoError(t, err)
	assert.Empty(t, prevState.Notifications)

	// Step 3: Filter changed MRs (all new)
	changed := filterChangedMRs(classified, prevState)
	require.Len(t, changed, 1, "New MR should be detected as changed")

	// Step 4: Resolve notification targets
	notifications := resolveNotificationTargets(changed, mapping)
	require.Len(t, notifications, 1)
	// needs_review targets reviewers; carol is a reviewer with mapping
	assert.Len(t, notifications[0].Targets, 1)
	assert.Equal(t, "U300", notifications[0].Targets[0].SlackID)
	assert.Equal(t, RoleReviewer, notifications[0].Targets[0].Role)

	// Step 5: Build and save state
	newState := buildNewState(classified)
	err = saveState(statePath, newState)
	require.NoError(t, err)

	// Verify state file was created
	_, err = os.Stat(statePath)
	require.NoError(t, err, "State file should exist after save")

	// Verify state file content
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var savedState NotificationState
	err = json.Unmarshal(data, &savedState)
	require.NoError(t, err)
	assert.Contains(t, savedState.Notifications, "5:101")
	assert.Equal(t, StatusNeedsReview, savedState.Notifications["5:101"].Status)
}

// TestExecuteSmartNotification_NoChanges tests that when the MR status
// has not changed since the last notification cycle, no notifications
// are generated but the state file is still updated.
func TestExecuteSmartNotification_NoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// 1 MR: needs_review
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                        101,
				Title:                      "Add dark mode",
				WebURL:                     "https://gitlab.com/test/project/-/merge_requests/101",
				Author:                     &gitlab.BasicUser{ID: 20, Name: "Bob", Username: "bob"},
				HasConflicts:               false,
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  5,
		},
	}

	// Pre-populate state file with the same MR status (needs_review)
	prevState := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"5:101": {Status: StatusNeedsReview, NotifiedAt: time.Now().Add(-1 * time.Hour)},
		},
	}
	err := saveState(statePath, prevState)
	require.NoError(t, err)

	// Classify
	classified := classifyMergeRequests(mrs)
	require.Len(t, classified, 1)
	assert.Equal(t, StatusNeedsReview, classified[0].Status)

	// Load state
	loadedState, err := loadState(statePath)
	require.NoError(t, err)

	// Filter changed → should be empty (same status)
	changed := filterChangedMRs(classified, loadedState)
	assert.Empty(t, changed, "No changes should be detected when status is the same")

	// State should still be saved (updated timestamps)
	newState := buildNewState(classified)
	err = saveState(statePath, newState)
	require.NoError(t, err)

	// Verify state file still exists and is updated
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var savedState NotificationState
	err = json.Unmarshal(data, &savedState)
	require.NoError(t, err)
	assert.Contains(t, savedState.Notifications, "5:101")
}

// TestExecuteSmartNotification_StateFileCreated verifies that
// executeSmartNotification creates a state file containing the MR key
// after processing.
func TestExecuteSmartNotification_StateFileCreated(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "notification_state.json")

	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                        55,
				Title:                      "Refactor auth module",
				WebURL:                     "https://gitlab.com/test/project/-/merge_requests/55",
				Author:                     &gitlab.BasicUser{ID: 5, Name: "Eve", Username: "eve"},
				HasConflicts:               false,
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  10,
		},
	}

	// Run the full pipeline
	classified := classifyMergeRequests(mrs)
	require.Len(t, classified, 1)

	prevState, err := loadState(statePath)
	require.NoError(t, err)

	changed := filterChangedMRs(classified, prevState)
	require.Len(t, changed, 1)

	newState := buildNewState(classified)
	err = saveState(statePath, newState)
	require.NoError(t, err)

	// Verify: state file exists
	info, err := os.Stat(statePath)
	require.NoError(t, err, "State file should be created")
	assert.True(t, info.Size() > 0, "State file should not be empty")

	// Verify: state file contains the MR key "10:55"
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var savedState NotificationState
	err = json.Unmarshal(data, &savedState)
	require.NoError(t, err)

	record, exists := savedState.Notifications["10:55"]
	require.True(t, exists, "State should contain key 10:55")
	assert.Equal(t, StatusNeedsReview, record.Status)
	assert.False(t, record.NotifiedAt.IsZero(), "NotifiedAt should be set")
}

// TestExecuteSmartNotification_NoStatePath verifies that the smart
// notification pipeline works correctly when no state path is configured
// (State: nil). In this case, state deduplication is disabled and all
// MRs are treated as new/changed.
func TestExecuteSmartNotification_NoStatePath(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                        77,
				Title:                      "Update CI pipeline",
				WebURL:                     "https://gitlab.com/test/project/-/merge_requests/77",
				Author:                     &gitlab.BasicUser{ID: 8, Name: "Frank", Username: "frank"},
				HasConflicts:               false,
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  15,
		},
	}

	mapping := []UserMappingEntry{
		{GitLabUsername: "frank", SlackID: "U800"},
	}

	// Step 1: Classify
	classified := classifyMergeRequests(mrs)
	require.Len(t, classified, 1)
	assert.Equal(t, StatusNeedsReview, classified[0].Status)

	// Step 2: Load state with empty path (State: nil scenario)
	prevState, err := loadState("")
	require.NoError(t, err, "loadState with empty path should not error")
	assert.NotNil(t, prevState)
	assert.Empty(t, prevState.Notifications)

	// Step 3: Filter changed (all MRs are new since state is empty)
	changed := filterChangedMRs(classified, prevState)
	require.Len(t, changed, 1, "All MRs should be treated as new when no state exists")

	// Step 4: Resolve targets
	notifications := resolveNotificationTargets(changed, mapping)
	require.Len(t, notifications, 1)
	assert.Contains(t, notifications[0].Message, "Update CI pipeline")

	// Step 5: Save state with empty path (should be a no-op)
	newState := buildNewState(classified)
	err = saveState("", newState)
	require.NoError(t, err, "saveState with empty path should be a no-op")
}

// timePtr is a helper to create a *time.Time value.
func timePtr(t time.Time) *time.Time {
	return &t
}

func TestShouldRunOneShot(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "no schedules configured → one-shot",
			config: &Config{},
			want:   true,
		},
		{
			name:   "summary nil → one-shot when cron empty",
			config: &Config{CronSchedule: "", Summary: nil},
			want:   true,
		},
		{
			name:   "cron schedule set → scheduler mode",
			config: &Config{CronSchedule: "0 9 * * *"},
			want:   false,
		},
		{
			name:   "summary schedule set → scheduler mode",
			config: &Config{Summary: &SummaryConfig{Schedule: "0 10 * * 1-5"}},
			want:   false,
		},
		{
			name:   "both schedules set → scheduler mode",
			config: &Config{CronSchedule: "*/5 * * * *", Summary: &SummaryConfig{Schedule: "0 9 * * *"}},
			want:   false,
		},
		{
			name:   "summary present but schedule empty → one-shot when cron empty",
			config: &Config{Summary: &SummaryConfig{Schedule: ""}},
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shouldRunOneShot(tc.config))
		})
	}
}
