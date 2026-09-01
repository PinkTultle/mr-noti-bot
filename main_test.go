package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func TestFilterMergeRequestsByAuthor_NilAuthorExcluded(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: nil, // 삭제된 사용자
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

	var filteredMRs []*MergeRequestWithApprovals
	require.NotPanics(t, func() {
		filteredMRs = filterMergeRequestsByAuthor(mrs, authors)
	})

	// author를 해석할 수 없는 MR은 어떤 author 필터에도 매칭되지 않아야 한다.
	require.Len(t, filteredMRs, 1)
	assert.Equal(t, 1, filteredMRs[0].MergeRequest.Author.ID)
}

func TestFilterMergeRequestsByAuthor_OptionalAuthors_NilAuthorPassesThrough(t *testing.T) {
	// authors 필터가 비어 있으면 Author를 역참조하지 않는 조기 반환 경로를 타므로,
	// nil Author의 MR도 그대로 통과해야 한다.
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Author: nil,
			},
		},
	}
	authors := []ConfigAuthor{}

	var filteredMRs []*MergeRequestWithApprovals
	require.NotPanics(t, func() {
		filteredMRs = filterMergeRequestsByAuthor(mrs, authors)
	})
	require.Len(t, filteredMRs, 1)
}

func TestFormatMergeRequestsSummary_NilAuthorDoesNotPanic(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				Title:     "ghost MR",
				WebURL:    "https://gl/7",
				Author:    nil, // 삭제된 사용자
				CreatedAt: timePtr(time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)),
			},
			ApprovedBy: []string{},
		},
	}

	var summary string
	require.NotPanics(t, func() {
		summary = formatMergeRequestsSummary(mrs)
	})
	assert.Contains(t, summary, "ghost MR")
	assert.Contains(t, summary, "unknown")
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

// TestExecute_AppliesAuthorFilterInSmartMode is an end-to-end regression test
// for the bug this commit fixes: filterMergeRequestsByAuthor used to run only
// inside executeLegacy, so adding a notification block silently re-enabled
// every author's MRs. execute() itself has no injectable GitLab client, so
// this drives the real execute() entry point against local HTTP stand-ins
// for GitLab and the Slack webhook — the fetch → filter → dispatch
// composition inside execute() runs completely unmodified.
func TestExecute_AppliesAuthorFilterInSmartMode(t *testing.T) {
	glMux := http.NewServeMux()
	glMux.HandleFunc("/api/v4/projects/1/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*gitlab.MergeRequest{
			{
				IID:                         1,
				Title:                       "alice의 MR",
				WebURL:                      "https://gitlab.example.com/test/project/-/merge_requests/1",
				Author:                      &gitlab.BasicUser{ID: 1, Name: "Alice", Username: "alice"},
				BlockingDiscussionsResolved: true,
			},
			{
				IID:                         2,
				Title:                       "bob의 MR",
				WebURL:                      "https://gitlab.example.com/test/project/-/merge_requests/2",
				Author:                      &gitlab.BasicUser{ID: 2, Name: "Bob", Username: "bob"},
				BlockingDiscussionsResolved: true,
			},
		})
	})
	emptyApprovals := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&gitlab.MergeRequestApprovals{
			ApprovedBy: []*gitlab.MergeRequestApproverUser{},
		})
	}
	glMux.HandleFunc("/api/v4/projects/1/merge_requests/1/approvals", emptyApprovals)
	glMux.HandleFunc("/api/v4/projects/1/merge_requests/2/approvals", emptyApprovals)
	glServer := httptest.NewServer(glMux)
	defer glServer.Close()

	var slackBodies []string
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		slackBodies = append(slackBodies, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	config := &Config{
		Projects: []ConfigProject{{ID: 1}},
		Authors:  []ConfigAuthor{{Username: "alice"}},
		Notification: &NotificationConfig{
			Mode:    "webhook",
			Webhook: &WebhookConfig{URL: slackServer.URL},
		},
		State: &StateConfig{Path: filepath.Join(t.TempDir(), "state.json")},
	}
	config.GitLab.URL = glServer.URL
	config.GitLab.Token = "test-token"

	require.NoError(t, execute(config))

	require.Len(t, slackBodies, 1)
	assert.Contains(t, slackBodies[0], "alice의 MR")
	assert.NotContains(t, slackBodies[0], "bob의 MR", "authors 필터가 execute()의 smart 경로에도 적용되어야 한다")
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

// TestExecuteSmartNotification_StateSaveFailureIsConsistent pins the fact
// that saveState itself already returns an error for an unwritable path.
// This is a regression guard, not the real fix verification: the actual
// bug lived in executeSmartNotification, where the two branches ("no
// changes" vs "changed") handled the same saveState failure differently.
// See TestExecuteSmartNotification_StateSaveFailure_NoChangesBranch and
// TestExecuteSmartNotification_StateSaveFailure_ChangedBranch below for
// that.
func TestExecuteSmartNotification_StateSaveFailureIsConsistent(t *testing.T) {
	// 쓰기 불가능한 경로
	badPath := filepath.Join(t.TempDir(), "no-such-dir", "state.json")

	config := &Config{
		State:        &StateConfig{Path: badPath},
		Notification: &NotificationConfig{Mode: "webhook", Webhook: &WebhookConfig{URL: "https://x"}},
		UserMapping:  []UserMappingEntry{{GitLabUsername: "alice", SlackID: "U1"}},
	}

	// 변경 있는 경우와 없는 경우 모두 error여야 한다
	err := saveState(config.State.Path, &NotificationState{})
	require.Error(t, err, "쓰기 불가 경로는 어느 분기에서든 error여야 한다")
}

// TestExecuteSmartNotification_StateSaveFailure_ChangedBranch drives
// executeSmartNotification through the "changed" branch with a state path
// that cannot be written (parent directory does not exist). Notifications
// must already have been sent by the time saveState fails, so the error
// must say so — the operator needs to know the next run will re-notify.
func TestExecuteSmartNotification_StateSaveFailure_ChangedBranch(t *testing.T) {
	var sent int
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	badPath := filepath.Join(t.TempDir(), "no-such-dir", "state.json")

	config := &Config{
		State:        &StateConfig{Path: badPath},
		Notification: &NotificationConfig{Mode: "webhook", Webhook: &WebhookConfig{URL: webhookServer.URL}},
		UserMapping:  []UserMappingEntry{{GitLabUsername: "bob", SlackID: "U200"}},
	}

	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                         101,
				Title:                       "Add dark mode",
				WebURL:                      "https://gitlab.com/test/project/-/merge_requests/101",
				Author:                      &gitlab.BasicUser{ID: 20, Name: "Bob", Username: "bob"},
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  5,
		},
	}

	err := executeSmartNotification(config, mrs)

	require.Error(t, err, "쓰기 불가 상태 경로는 changed 분기에서도 error를 반환해야 한다")
	assert.Contains(t, err.Error(), "notifications sent but state was not saved",
		"알림이 이미 발송됐다는 사실이 에러 메시지에 남아야 한다")
	assert.Equal(t, 1, sent, "state 저장에 실패하기 전에 알림은 이미 발송돼 있어야 한다")
}

// TestExecuteSmartNotification_StateSaveFailure_NoChangesBranch drives
// executeSmartNotification through the "no changes" branch with a state
// file that exists and is readable (so the MR is correctly classified as
// unchanged) but whose directory is not writable, so the save at the end
// fails. Before this fix this branch already returned an error; this test
// pins that behavior and checks it happens for the same underlying failure
// used in the ChangedBranch test above.
func TestExecuteSmartNotification_StateSaveFailure_NoChangesBranch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permission is not reliably enforceable via chmod on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root bypasses directory write permission checks")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                         101,
				Title:                       "Add dark mode",
				WebURL:                      "https://gitlab.com/test/project/-/merge_requests/101",
				Author:                      &gitlab.BasicUser{ID: 20, Name: "Bob", Username: "bob"},
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
			ProjectID:  5,
		},
	}

	// classified 결과와 동일한 status로 미리 state를 저장해 "변경 없음"이
	// 되도록 만든다.
	classified := classifyMergeRequests(mrs)
	require.Len(t, classified, 1)
	err := saveState(statePath, &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"5:101": {Status: classified[0].Status, NotifiedAt: time.Now()},
		},
	})
	require.NoError(t, err)

	// 디렉토리를 읽기 전용으로 바꿔 saveState의 임시 파일 생성이 실패하게
	// 한다. 기존 state.json 파일은 여전히 읽을 수 있다.
	require.NoError(t, os.Chmod(tmpDir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(tmpDir, 0o755) // t.TempDir() 정리를 위해 권한 복구
	})

	config := &Config{State: &StateConfig{Path: statePath}}

	err = executeSmartNotification(config, mrs)

	require.Error(t, err, "쓰기 불가 상태 경로는 no-changes 분기에서도 error를 반환해야 한다")
	assert.Contains(t, err.Error(), "error saving state")
}

func TestLambdaInput_SelectsSummaryJob(t *testing.T) {
	assert.Equal(t, "summary", LambdaInput{Job: "summary"}.normalizedJob())
	assert.Equal(t, "notify", LambdaInput{}.normalizedJob(), "빈 값은 기본 알림 잡이다")
	assert.Equal(t, "notify", LambdaInput{Job: "notify"}.normalizedJob())
}

func TestLambdaInput_UnknownJobIsRejected(t *testing.T) {
	_, err := runLambdaJob(&Config{}, LambdaInput{Job: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestRunLambdaJob_NotifySucceedsWithoutNetwork(t *testing.T) {
	// 빈 Config는 groups/projects가 없어 execute()가 GitLab API를 전혀 호출하지 않고
	// "MR 없음" 분기로 바로 성공한다 — notify가 실제로 execute()에 도달했음을 증명한다.
	res, err := runLambdaJob(&Config{}, LambdaInput{Job: "notify"})
	require.NoError(t, err)
	assert.Equal(t, "Success", res)
}

func TestRunLambdaJob_SummaryReachesSummaryValidation(t *testing.T) {
	// 빈 Config는 webhook 설정이 없어 executeSummary()가 네트워크보다 먼저
	// 이 에러로 실패한다 — summary가 execute()가 아니라 executeSummary()에
	// 도달했음을, notify와는 다른 에러로 구분해 증명한다.
	_, err := runLambdaJob(&Config{}, LambdaInput{Job: "summary"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no webhook URL configured for summary")
}
