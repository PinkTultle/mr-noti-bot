package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// makeClassifiedMR creates a ClassifiedMR for testing with minimal fields.
func makeClassifiedMR(projectID, iid int64, status MRStatus) *ClassifiedMR {
	return &ClassifiedMR{
		MR: &MergeRequestWithApprovals{
			MergeRequest: &gitlab.BasicMergeRequest{IID: iid},
			ProjectID:    projectID,
		},
		Status: status,
	}
}

func TestStateKey(t *testing.T) {
	result := stateKey(123, 1001)
	assert.Equal(t, "123:1001", result)
}

func TestLoadState_EmptyPath(t *testing.T) {
	state, err := loadState("")

	require.NoError(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.Notifications)
	assert.Empty(t, state.Notifications)
}

func TestLoadState_FileNotExist(t *testing.T) {
	state, err := loadState(filepath.Join(t.TempDir(), "nonexistent.json"))

	require.NoError(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.Notifications)
	assert.Empty(t, state.Notifications)
}

func TestSaveAndLoadState_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"10:100": {Status: StatusNeedsReview},
			"20:200": {Status: StatusApprovedPendingMerge},
		},
	}

	err := saveState(path, original)
	require.NoError(t, err)

	loaded, err := loadState(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Len(t, loaded.Notifications, 2)
	assert.Equal(t, StatusNeedsReview, loaded.Notifications["10:100"].Status)
	assert.Equal(t, StatusApprovedPendingMerge, loaded.Notifications["20:200"].Status)
	assert.False(t, loaded.UpdatedAt.IsZero(), "UpdatedAt should be set after save")
}

func TestSaveState_EmptyPath(t *testing.T) {
	state := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"10:100": {Status: StatusNeedsReview},
		},
	}

	err := saveState("", state)
	assert.NoError(t, err)
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	err := os.WriteFile(path, []byte("not valid json"), 0644)
	require.NoError(t, err)

	state, err := loadState(path)
	assert.Error(t, err)
	assert.Nil(t, state)
	assert.True(t, isStateCorrupted(err), "invalid JSON must be classified as corrupted state")
}

func TestSaveState_IsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// 기존 유효 상태
	require.NoError(t, saveState(path, &NotificationState{
		Notifications: map[string]MRNotificationRecord{"1:1": {Status: StatusNeedsReview}},
	}))

	// 저장 도중 임시 파일이 남더라도 대상 파일은 항상 유효한 JSON이어야 한다
	require.NoError(t, saveState(path, &NotificationState{
		Notifications: map[string]MRNotificationRecord{"1:2": {Status: StatusHasConflicts}},
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var parsed NotificationState
	require.NoError(t, json.Unmarshal(data, &parsed), "상태 파일은 항상 유효한 JSON이어야 한다")

	// 임시 파일이 남아있지 않아야 한다
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "임시 파일이 정리되지 않았다")
}

func TestLoadState_CorruptedFileIsDistinguishable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"notifications": {"1:1"`), 0644)) // 잘린 JSON

	_, err := loadState(path)
	require.Error(t, err)
	assert.True(t, isStateCorrupted(err), "파싱 실패는 손상으로 식별되어야 한다")
}

func TestLoadState_MissingFileIsNotAnError(t *testing.T) {
	state, err := loadState(filepath.Join(t.TempDir(), "absent.json"))
	require.NoError(t, err)
	assert.Empty(t, state.Notifications)
}

func TestLoadState_NilNotificationsMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "null-map.json")

	// JSON with null notifications field
	err := os.WriteFile(path, []byte(`{"notifications": null, "updated_at": "2026-01-01T00:00:00Z"}`), 0644)
	require.NoError(t, err)

	state, err := loadState(path)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.Notifications, "Notifications map should be initialized even if null in JSON")
}

func TestFilterChangedMRs_NewMR(t *testing.T) {
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusNeedsReview),
	}
	prevState := &NotificationState{
		Notifications: make(map[string]MRNotificationRecord),
	}

	result := filterChangedMRs(classified, prevState)

	require.Len(t, result, 1)
	assert.Equal(t, classified[0], result[0])
}

func TestFilterChangedMRs_StatusChanged(t *testing.T) {
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusApprovedPendingMerge),
	}
	prevState := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"10:100": {Status: StatusNeedsReview},
		},
	}

	result := filterChangedMRs(classified, prevState)

	require.Len(t, result, 1)
	assert.Equal(t, classified[0], result[0])
}

func TestFilterChangedMRs_StatusSame(t *testing.T) {
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusNeedsReview),
	}
	prevState := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"10:100": {Status: StatusNeedsReview},
		},
	}

	result := filterChangedMRs(classified, prevState)

	assert.Empty(t, result)
}

func TestFilterChangedMRs_Mixed(t *testing.T) {
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusNeedsReview),          // new
		makeClassifiedMR(10, 200, StatusApprovedPendingMerge), // changed
		makeClassifiedMR(10, 300, StatusHasConflicts),         // same
	}
	prevState := &NotificationState{
		Notifications: map[string]MRNotificationRecord{
			"10:200": {Status: StatusNeedsReview},
			"10:300": {Status: StatusHasConflicts},
		},
	}

	result := filterChangedMRs(classified, prevState)

	require.Len(t, result, 2)
	assert.Equal(t, classified[0], result[0], "new MR should be included")
	assert.Equal(t, classified[1], result[1], "changed MR should be included")
}

func TestBuildNewState(t *testing.T) {
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusNeedsReview),
		makeClassifiedMR(20, 200, StatusApprovedPendingMerge),
	}

	state := buildNewState(classified)

	require.NotNil(t, state)
	assert.Len(t, state.Notifications, 2)
	assert.Equal(t, StatusNeedsReview, state.Notifications["10:100"].Status)
	assert.Equal(t, StatusApprovedPendingMerge, state.Notifications["20:200"].Status)
	assert.False(t, state.UpdatedAt.IsZero())
}

func TestBuildNewState_StaleCleaned(t *testing.T) {
	// Previous state had MR 10:300 which is no longer in the classified list
	classified := []*ClassifiedMR{
		makeClassifiedMR(10, 100, StatusNeedsReview),
	}

	state := buildNewState(classified)

	require.NotNil(t, state)
	assert.Len(t, state.Notifications, 1)
	assert.Contains(t, state.Notifications, "10:100")

	// Stale entry should not exist in new state
	_, exists := state.Notifications["10:300"]
	assert.False(t, exists, "stale MR 10:300 should not be in new state")
}

func TestBuildNewState_EmptyInput(t *testing.T) {
	state := buildNewState(nil)

	require.NotNil(t, state)
	assert.Empty(t, state.Notifications)
	assert.False(t, state.UpdatedAt.IsZero())
}
