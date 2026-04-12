package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// NotificationState holds the state of previously sent notifications
// for deduplication across polling cycles.
type NotificationState struct {
	Notifications map[string]MRNotificationRecord `json:"notifications"`
	UpdatedAt     time.Time                       `json:"updated_at"`
}

// MRNotificationRecord stores the last notified status for a single MR.
type MRNotificationRecord struct {
	Status     MRStatus  `json:"status"`
	NotifiedAt time.Time `json:"notified_at"`
}

// stateKey generates a unique key for a MR: "ProjectID:MR_IID"
func stateKey(projectID, mrIID int) string {
	return fmt.Sprintf("%d:%d", projectID, mrIID)
}

// loadState reads the notification state from a JSON file.
// Returns empty state if path is empty or file doesn't exist.
func loadState(path string) (*NotificationState, error) {
	if path == "" {
		return &NotificationState{Notifications: make(map[string]MRNotificationRecord)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &NotificationState{Notifications: make(map[string]MRNotificationRecord)}, nil
		}
		return nil, fmt.Errorf("error reading state file: %w", err)
	}

	var state NotificationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("error parsing state file: %w", err)
	}

	if state.Notifications == nil {
		state.Notifications = make(map[string]MRNotificationRecord)
	}

	return &state, nil
}

// saveState writes the notification state to a JSON file.
// No-op if path is empty.
func saveState(path string, state *NotificationState) error {
	if path == "" {
		return nil
	}

	state.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("error writing state file: %w", err)
	}

	return nil
}

// filterChangedMRs compares classified MRs against previous state
// and returns only those whose status has changed (or are new).
func filterChangedMRs(classified []*ClassifiedMR, prevState *NotificationState) []*ClassifiedMR {
	var changed []*ClassifiedMR
	for _, cmr := range classified {
		key := stateKey(cmr.MR.ProjectID, cmr.MR.MergeRequest.IID)
		prev, exists := prevState.Notifications[key]
		if !exists || prev.Status != cmr.Status {
			changed = append(changed, cmr)
		}
	}
	return changed
}

// buildNewState creates a fresh NotificationState from the current list
// of classified MRs. MRs no longer in the list are automatically removed (stale cleanup).
func buildNewState(classified []*ClassifiedMR) *NotificationState {
	state := &NotificationState{
		Notifications: make(map[string]MRNotificationRecord),
		UpdatedAt:     time.Now(),
	}
	for _, cmr := range classified {
		key := stateKey(cmr.MR.ProjectID, cmr.MR.MergeRequest.IID)
		state.Notifications[key] = MRNotificationRecord{
			Status:     cmr.Status,
			NotifiedAt: time.Now(),
		}
	}
	return state
}
