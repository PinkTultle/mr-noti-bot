package main

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
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
	defer log.SetOutput(nil)

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
	defer log.SetOutput(nil)

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
