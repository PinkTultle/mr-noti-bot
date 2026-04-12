package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"
)

func TestClassifyMergeRequest_HasConflicts(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                true,
			BlockingDiscussionsResolved: true,
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusHasConflicts, result.Status)
	assert.Equal(t, []TargetRole{RoleAuthor}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_BlockingDiscussions(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: false,
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusBlockingDiscussions, result.Status)
	assert.Equal(t, []TargetRole{RoleAuthor, RoleReviewer}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_ChangesRequested(t *testing.T) {
	// Partial approval: 1 of 2 reviewers approved → changes_requested
	// Priority 2 catches this before blocking_discussions check
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{ID: 1, Name: "Reviewer A"},
				{ID: 2, Name: "Reviewer B"},
			},
		},
		ApprovedBy: []string{"Reviewer A"},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusChangesRequested, result.Status)
	assert.Equal(t, []TargetRole{RoleAuthor}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_ApprovedPendingMerge(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{ID: 1, Name: "Reviewer A"},
			},
		},
		ApprovedBy: []string{"Reviewer A"},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusApprovedPendingMerge, result.Status)
	assert.Equal(t, []TargetRole{RoleAuthor}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_NeedsReview(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{ID: 1, Name: "Reviewer A"},
			},
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusNeedsReview, result.Status)
	assert.Equal(t, []TargetRole{RoleReviewer}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_ConflictTakesPriority(t *testing.T) {
	// Both HasConflicts=true AND BlockingDiscussionsResolved=false
	// HasConflicts should win due to higher priority
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                true,
			BlockingDiscussionsResolved: false,
			Reviewers: []*gitlab.BasicUser{
				{ID: 1, Name: "Reviewer A"},
			},
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusHasConflicts, result.Status)
	assert.Equal(t, []TargetRole{RoleAuthor}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_NoReviewers(t *testing.T) {
	// MR with no reviewers and no approvals → needs_review with [reviewer] target.
	// Even though there are no actual reviewers to notify, the classification
	// still marks the MR as needs_review targeting the reviewer role.
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: true,
			Reviewers:                   []*gitlab.BasicUser{}, // empty — no reviewers assigned
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusNeedsReview, result.Status)
	assert.Equal(t, []TargetRole{RoleReviewer}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequest_NoReviewersNilSlice(t *testing.T) {
	// Same as above but with nil Reviewers slice instead of empty.
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{
			HasConflicts:                false,
			BlockingDiscussionsResolved: true,
			Reviewers:                   nil,
		},
		ApprovedBy: []string{},
	}

	result := classifyMergeRequest(mr)

	require.NotNil(t, result)
	assert.Equal(t, StatusNeedsReview, result.Status)
	assert.Equal(t, []TargetRole{RoleReviewer}, result.TargetRoles)
	assert.Equal(t, mr, result.MR)
}

func TestClassifyMergeRequests_Batch(t *testing.T) {
	mrs := []*MergeRequestWithApprovals{
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                         1,
				HasConflicts:                true,
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
		},
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                         2,
				HasConflicts:                false,
				BlockingDiscussionsResolved: true,
				Reviewers: []*gitlab.BasicUser{
					{ID: 1, Name: "Reviewer A"},
				},
			},
			ApprovedBy: []string{},
		},
		{
			MergeRequest: &gitlab.MergeRequest{
				IID:                         3,
				HasConflicts:                false,
				BlockingDiscussionsResolved: true,
				Reviewers: []*gitlab.BasicUser{
					{ID: 1, Name: "Reviewer A"},
				},
			},
			ApprovedBy: []string{"Reviewer A"},
		},
	}

	results := classifyMergeRequests(mrs)

	require.Len(t, results, 3)

	assert.Equal(t, StatusHasConflicts, results[0].Status)
	assert.Equal(t, mrs[0], results[0].MR)

	assert.Equal(t, StatusNeedsReview, results[1].Status)
	assert.Equal(t, mrs[1], results[1].MR)

	assert.Equal(t, StatusApprovedPendingMerge, results[2].Status)
	assert.Equal(t, mrs[2], results[2].MR)
}
