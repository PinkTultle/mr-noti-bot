package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestClassifyMergeRequest_HasConflicts(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.BasicMergeRequest{
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
		MergeRequest: &gitlab.BasicMergeRequest{
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

func TestClassify_PartialApprovalNotifiesPendingReviewer(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		ApprovedBy: []string{"Carol"}, // 2명 중 1명 승인
		MergeRequest: &gitlab.BasicMergeRequest{
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{Username: "carol"}, {Username: "dave"},
			},
		},
	}

	got := classifyMergeRequest(mr)

	assert.Equal(t, StatusNeedsReview, got.Status,
		"아무도 변경을 요청하지 않았는데 changes_requested로 분류하면 안 된다")
	assert.Contains(t, got.TargetRoles, RoleReviewer,
		"아직 승인하지 않은 리뷰어에게 알림이 가야 한다")
	assert.NotContains(t, got.TargetRoles, RoleAuthor,
		"작성자에게 변경 요청 알림이 가면 안 된다")
}

func TestClassify_AllReviewersApproved(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		ApprovedBy: []string{"Carol", "Dave"},
		MergeRequest: &gitlab.BasicMergeRequest{
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{Username: "carol"}, {Username: "dave"},
			},
		},
	}

	got := classifyMergeRequest(mr)
	assert.Equal(t, StatusApprovedPendingMerge, got.Status)
	assert.Equal(t, []TargetRole{RoleAuthor}, got.TargetRoles)
}

func TestClassify_ApprovalFromNonReviewer(t *testing.T) {
	// 리뷰어로 지정되지 않은 메인테이너가 승인한 경우.
	// 예전에는 3 > 1 비교로 작성자에게 헛알림이 갔다.
	mr := &MergeRequestWithApprovals{
		ApprovedBy: []string{"Maintainer"},
		MergeRequest: &gitlab.BasicMergeRequest{
			BlockingDiscussionsResolved: true,
			Reviewers: []*gitlab.BasicUser{
				{Username: "a"}, {Username: "b"}, {Username: "c"},
			},
		},
	}

	got := classifyMergeRequest(mr)
	assert.NotEqual(t, StatusChangesRequested, got.Status)
}

func TestClassifyMergeRequest_ApprovedPendingMerge(t *testing.T) {
	mr := &MergeRequestWithApprovals{
		MergeRequest: &gitlab.BasicMergeRequest{
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
		MergeRequest: &gitlab.BasicMergeRequest{
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
		MergeRequest: &gitlab.BasicMergeRequest{
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
		MergeRequest: &gitlab.BasicMergeRequest{
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
		MergeRequest: &gitlab.BasicMergeRequest{
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
			MergeRequest: &gitlab.BasicMergeRequest{
				IID:                         1,
				HasConflicts:                true,
				BlockingDiscussionsResolved: true,
			},
			ApprovedBy: []string{},
		},
		{
			MergeRequest: &gitlab.BasicMergeRequest{
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
			MergeRequest: &gitlab.BasicMergeRequest{
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
