package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"
)

// helper to create a ClassifiedMR for testing.
func newTestClassifiedMR(
	author *gitlab.BasicUser,
	reviewers []*gitlab.BasicUser,
	status MRStatus,
	roles []TargetRole,
) *ClassifiedMR {
	mr := &gitlab.MergeRequest{
		Title:  "Test MR",
		WebURL: "https://gitlab.com/test/project/-/merge_requests/1",
		Author: author,
	}
	if reviewers != nil {
		mr.Reviewers = reviewers
	}
	return &ClassifiedMR{
		MR: &MergeRequestWithApprovals{
			MergeRequest: mr,
		},
		Status:      status,
		TargetRoles: roles,
	}
}

func TestResolveNotificationTargets_AuthorMapped(t *testing.T) {
	author := &gitlab.BasicUser{Username: "alice", Name: "Alice"}
	cmr := newTestClassifiedMR(author, nil, StatusNeedsReview, []TargetRole{RoleAuthor})

	mapping := []UserMappingEntry{
		{GitLabUsername: "alice", SlackID: "U001"},
	}

	notifications := resolveNotificationTargets([]*ClassifiedMR{cmr}, mapping)

	assert.Len(t, notifications, 1)
	assert.Len(t, notifications[0].Targets, 1)
	assert.Equal(t, "U001", notifications[0].Targets[0].SlackID)
	assert.Equal(t, "alice", notifications[0].Targets[0].GitLabUsername)
	assert.Equal(t, RoleAuthor, notifications[0].Targets[0].Role)
	assert.Contains(t, notifications[0].Message, "리뷰를 기다리고 있습니다")
	assert.Contains(t, notifications[0].Message, "Test MR")
}

func TestResolveNotificationTargets_ReviewersMapped(t *testing.T) {
	author := &gitlab.BasicUser{Username: "alice", Name: "Alice"}
	reviewers := []*gitlab.BasicUser{
		{Username: "bob", Name: "Bob"},
		{Username: "carol", Name: "Carol"},
	}
	cmr := newTestClassifiedMR(author, reviewers, StatusNeedsReview, []TargetRole{RoleReviewer})

	mapping := []UserMappingEntry{
		{GitLabUsername: "bob", SlackID: "U002"},
		{GitLabUsername: "carol", SlackID: "U003"},
	}

	notifications := resolveNotificationTargets([]*ClassifiedMR{cmr}, mapping)

	assert.Len(t, notifications, 1)
	assert.Len(t, notifications[0].Targets, 2)
	assert.Equal(t, "U002", notifications[0].Targets[0].SlackID)
	assert.Equal(t, RoleReviewer, notifications[0].Targets[0].Role)
	assert.Equal(t, "U003", notifications[0].Targets[1].SlackID)
	assert.Equal(t, RoleReviewer, notifications[0].Targets[1].Role)
}

func TestResolveNotificationTargets_MappingMissing(t *testing.T) {
	// Capture log output to verify warning
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	author := &gitlab.BasicUser{Username: "unknown_user", Name: "Unknown"}
	cmr := newTestClassifiedMR(author, nil, StatusNeedsReview, []TargetRole{RoleAuthor})

	mapping := []UserMappingEntry{
		{GitLabUsername: "alice", SlackID: "U001"},
	}

	notifications := resolveNotificationTargets([]*ClassifiedMR{cmr}, mapping)

	assert.Len(t, notifications, 1)
	assert.Empty(t, notifications[0].Targets)
	assert.Contains(t, buf.String(), "no Slack mapping for GitLab user")
	assert.Contains(t, buf.String(), "unknown_user")
}

func TestResolveNotificationTargets_PartialMapping(t *testing.T) {
	author := &gitlab.BasicUser{Username: "alice", Name: "Alice"}
	reviewers := []*gitlab.BasicUser{
		{Username: "bob", Name: "Bob"},
		{Username: "carol", Name: "Carol"},
		{Username: "dave", Name: "Dave"},
	}
	cmr := newTestClassifiedMR(author, reviewers, StatusChangesRequested, []TargetRole{RoleReviewer})

	// Only 2 of 3 reviewers are mapped
	mapping := []UserMappingEntry{
		{GitLabUsername: "bob", SlackID: "U002"},
		{GitLabUsername: "carol", SlackID: "U003"},
	}

	notifications := resolveNotificationTargets([]*ClassifiedMR{cmr}, mapping)

	assert.Len(t, notifications, 1)
	assert.Len(t, notifications[0].Targets, 2)
	assert.Equal(t, "bob", notifications[0].Targets[0].GitLabUsername)
	assert.Equal(t, "carol", notifications[0].Targets[1].GitLabUsername)
}

func TestResolveNotificationTargets_EmptyMapping(t *testing.T) {
	author := &gitlab.BasicUser{Username: "alice", Name: "Alice"}
	cmr := newTestClassifiedMR(author, nil, StatusNeedsReview, []TargetRole{RoleAuthor})

	// Empty mapping - should not log warnings
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	notifications := resolveNotificationTargets([]*ClassifiedMR{cmr}, []UserMappingEntry{})

	assert.Len(t, notifications, 1)
	assert.Empty(t, notifications[0].Targets)
	// No warning should be logged when mapping is empty
	assert.Empty(t, buf.String())
}

func TestFormatMentions(t *testing.T) {
	targets := []NotificationTarget{
		{SlackID: "U123", GitLabUsername: "alice", Role: RoleAuthor},
		{SlackID: "U456", GitLabUsername: "bob", Role: RoleReviewer},
	}

	result := formatMentions(targets)
	assert.Equal(t, "<@U123> <@U456>", result)
}

func TestFormatMentions_Empty(t *testing.T) {
	result := formatMentions([]NotificationTarget{})
	assert.Equal(t, "", result)
}

func TestResolveNotificationTargets_NilAuthorDoesNotPanic(t *testing.T) {
	classified := []*ClassifiedMR{{
		Status:      StatusNeedsReview,
		TargetRoles: []TargetRole{RoleAuthor, RoleReviewer},
		MR: &MergeRequestWithApprovals{
			ProjectID: 1,
			MergeRequest: &gitlab.MergeRequest{
				IID: 7, Title: "ghost MR", WebURL: "https://gl/7",
				Author:    nil, // 삭제된 사용자
				Reviewers: []*gitlab.BasicUser{{Username: "carol"}},
			},
		},
	}}

	var notifications []*Notification
	require.NotPanics(t, func() {
		notifications = resolveNotificationTargets(classified,
			[]UserMappingEntry{{GitLabUsername: "carol", SlackID: "U_CAROL"}})
	})

	require.Len(t, notifications, 1)
	assert.Contains(t, notifications[0].Message, "ghost MR")
	// author를 해석할 수 없어도 리뷰어 알림은 살아있어야 한다
	assert.Len(t, notifications[0].Targets, 1)
	assert.Equal(t, "U_CAROL", notifications[0].Targets[0].SlackID)
}

func TestResolveNotificationTargets_DedupesAuthorWhoIsAlsoReviewer(t *testing.T) {
	classified := []*ClassifiedMR{{
		Status:      StatusBlockingDiscussions,
		TargetRoles: []TargetRole{RoleAuthor, RoleReviewer},
		MR: &MergeRequestWithApprovals{
			ProjectID: 1,
			MergeRequest: &gitlab.MergeRequest{
				IID: 1, Title: "t", WebURL: "https://gl/1",
				Author:    &gitlab.BasicUser{Username: "alice", Name: "Alice"},
				Reviewers: []*gitlab.BasicUser{{Username: "alice"}, {Username: "bob"}},
			},
		},
	}}

	notifications := resolveNotificationTargets(classified, []UserMappingEntry{
		{GitLabUsername: "alice", SlackID: "U_ALICE"},
		{GitLabUsername: "bob", SlackID: "U_BOB"},
	})

	require.Len(t, notifications, 1)
	assert.Len(t, notifications[0].Targets, 2, "alice가 작성자이자 리뷰어여도 한 번만 들어가야 한다")

	mentions := formatMentions(notifications[0].Targets)
	assert.Equal(t, 1, strings.Count(mentions, "<@U_ALICE>"))
	assert.Contains(t, mentions, "<@U_BOB>")
}

func TestResolveNotificationTargets_DedupesReviewersSharingSlackID(t *testing.T) {
	// 서로 다른 GitLab 계정이 같은 Slack ID에 매핑된 경우에도
	// 리뷰어 목록 안에서 중복이 제거되어야 한다.
	classified := []*ClassifiedMR{{
		Status:      StatusNeedsReview,
		TargetRoles: []TargetRole{RoleReviewer},
		MR: &MergeRequestWithApprovals{
			ProjectID: 1,
			MergeRequest: &gitlab.MergeRequest{
				IID: 2, Title: "t2", WebURL: "https://gl/2",
				Author:    &gitlab.BasicUser{Username: "alice", Name: "Alice"},
				Reviewers: []*gitlab.BasicUser{{Username: "dave"}, {Username: "dave2"}},
			},
		},
	}}

	notifications := resolveNotificationTargets(classified, []UserMappingEntry{
		{GitLabUsername: "dave", SlackID: "U_DAVE"},
		{GitLabUsername: "dave2", SlackID: "U_DAVE"},
	})

	require.Len(t, notifications, 1)
	require.Len(t, notifications[0].Targets, 1)
	assert.Equal(t, "U_DAVE", notifications[0].Targets[0].SlackID)
	assert.Equal(t, "dave", notifications[0].Targets[0].GitLabUsername,
		"먼저 나온 리뷰어(dave)가 유지되어야 한다")
}

func TestResolveNotificationTargets_DedupeIsPerMR(t *testing.T) {
	// seen 맵은 MR마다 새로 만들어져야 한다 — 공유되면 두 번째 MR부터
	// 동일 인물이 잘못 걸러진다.
	makeMR := func(iid int) *ClassifiedMR {
		return &ClassifiedMR{
			Status:      StatusNeedsReview,
			TargetRoles: []TargetRole{RoleReviewer},
			MR: &MergeRequestWithApprovals{
				ProjectID: 1,
				MergeRequest: &gitlab.MergeRequest{
					IID: iid, Title: "t", WebURL: "https://gl/mr",
					Author:    &gitlab.BasicUser{Username: "alice", Name: "Alice"},
					Reviewers: []*gitlab.BasicUser{{Username: "bob"}},
				},
			},
		}
	}
	classified := []*ClassifiedMR{makeMR(1), makeMR(2)}

	notifications := resolveNotificationTargets(classified, []UserMappingEntry{
		{GitLabUsername: "bob", SlackID: "U_BOB"},
	})

	require.Len(t, notifications, 2)
	for i, n := range notifications {
		require.Len(t, n.Targets, 1, "MR %d에서 bob이 사라지면 안 된다", i)
		assert.Equal(t, "U_BOB", n.Targets[0].SlackID)
	}
}

func TestFormatNotificationLine_NilAuthor(t *testing.T) {
	n := &Notification{MR: &ClassifiedMR{MR: &MergeRequestWithApprovals{
		MergeRequest: &gitlab.MergeRequest{Title: "t", WebURL: "u", Author: nil},
	}}}
	require.NotPanics(t, func() { _ = formatNotificationLine(n) })
	assert.Contains(t, formatNotificationLine(n), "unknown")
}
