package main

// MRStatus represents the classification status of a merge request.
type MRStatus string

const (
	StatusNeedsReview          MRStatus = "needs_review"
	StatusChangesRequested     MRStatus = "changes_requested"
	StatusApprovedPendingMerge MRStatus = "approved_pending_merge"
	StatusHasConflicts         MRStatus = "has_conflicts"
	StatusBlockingDiscussions  MRStatus = "blocking_discussions"
)

// TargetRole represents who should act on a merge request.
type TargetRole string

const (
	RoleAuthor   TargetRole = "author"
	RoleReviewer TargetRole = "reviewer"
)

// ClassifiedMR holds a merge request along with its classification result.
type ClassifiedMR struct {
	MR          *MergeRequestWithApprovals
	Status      MRStatus
	TargetRoles []TargetRole
}

// classifyMergeRequest determines the status and target roles for a single MR.
// Classification follows a priority order: the first matching rule wins.
func classifyMergeRequest(mr *MergeRequestWithApprovals) *ClassifiedMR {
	// Priority 1: Merge conflicts block everything
	if mr.MergeRequest.HasConflicts {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusHasConflicts,
			TargetRoles: []TargetRole{RoleAuthor},
		}
	}

	// Priority 2: Unresolved blocking discussions
	if !mr.MergeRequest.BlockingDiscussionsResolved {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusBlockingDiscussions,
			TargetRoles: []TargetRole{RoleAuthor, RoleReviewer},
		}
	}

	// Priority 3: 지정된 리뷰어가 전원 승인했고 논의도 정리된 상태 → 머지 대기.
	// ApprovedBy는 승인자 이름, Reviewers는 지정된 리뷰어라 개수 비교는
	// 신뢰할 수 없다. 리뷰어가 지정되지 않은 MR은 승인 1건으로 머지 대기로 본다.
	if len(mr.ApprovedBy) > 0 {
		reviewerCount := len(mr.MergeRequest.Reviewers)
		if reviewerCount == 0 || len(mr.ApprovedBy) >= reviewerCount {
			return &ClassifiedMR{
				MR:          mr,
				Status:      StatusApprovedPendingMerge,
				TargetRoles: []TargetRole{RoleAuthor},
			}
		}
		// 부분 승인 — 아직 승인하지 않은 리뷰어를 기다리는 중이다.
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusNeedsReview,
			TargetRoles: []TargetRole{RoleReviewer},
		}
	}

	// Priority 4: 승인 없음
	return &ClassifiedMR{
		MR:          mr,
		Status:      StatusNeedsReview,
		TargetRoles: []TargetRole{RoleReviewer},
	}
}

// classifyMergeRequests classifies a batch of merge requests.
func classifyMergeRequests(mrs []*MergeRequestWithApprovals) []*ClassifiedMR {
	classified := make([]*ClassifiedMR, 0, len(mrs))
	for _, mr := range mrs {
		classified = append(classified, classifyMergeRequest(mr))
	}
	return classified
}
