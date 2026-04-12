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

	// Priority 2: Partial approval — some reviewers approved, but not all.
	// Indicates changes were requested by non-approving reviewers.
	if len(mr.ApprovedBy) > 0 &&
		len(mr.ApprovedBy) < len(mr.MergeRequest.Reviewers) {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusChangesRequested,
			TargetRoles: []TargetRole{RoleAuthor},
		}
	}

	// Priority 3: Unresolved blocking discussions
	if !mr.MergeRequest.BlockingDiscussionsResolved {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusBlockingDiscussions,
			TargetRoles: []TargetRole{RoleAuthor, RoleReviewer},
		}
	}

	// Priority 4: At least one approval and all discussions resolved
	if len(mr.ApprovedBy) >= 1 && mr.MergeRequest.BlockingDiscussionsResolved {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusApprovedPendingMerge,
			TargetRoles: []TargetRole{RoleAuthor},
		}
	}

	// Priority 5: No approvals yet
	if len(mr.ApprovedBy) == 0 {
		return &ClassifiedMR{
			MR:          mr,
			Status:      StatusNeedsReview,
			TargetRoles: []TargetRole{RoleReviewer},
		}
	}

	// Default fallback
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
