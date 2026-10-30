package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// authorName returns the MR author display name, or "unknown" when the
// author is absent (deleted user, or a service account the token cannot resolve).
func authorName(mr *gitlab.BasicMergeRequest) string {
	if mr == nil || mr.Author == nil {
		return "unknown"
	}
	return mr.Author.Name
}

// authorUsername returns the MR author username, or "" when absent.
func authorUsername(mr *gitlab.BasicMergeRequest) string {
	if mr == nil || mr.Author == nil {
		return ""
	}
	return mr.Author.Username
}

// mrCreatedAt returns the MR creation time, or ok=false when absent.
func mrCreatedAt(mr *gitlab.BasicMergeRequest) (time.Time, bool) {
	if mr == nil || mr.CreatedAt == nil {
		return time.Time{}, false
	}
	return *mr.CreatedAt, true
}

// NotificationTarget represents a single user to notify, resolved from
// GitLab username to Slack ID via user_mapping configuration.
type NotificationTarget struct {
	SlackID        string
	GitLabUsername string
	Role           TargetRole
}

// Notification groups a classified MR with its resolved targets and a
// pre-formatted message ready for delivery.
type Notification struct {
	MR      *ClassifiedMR
	Targets []NotificationTarget
	Message string
}

// statusMessages maps each MR status to a human-readable Slack message prefix.
var statusMessages = map[MRStatus]string{
	StatusNeedsReview:          ":eyes: 리뷰를 기다리고 있습니다",
	StatusChangesRequested:     ":pencil2: 변경 요청된 피드백이 있습니다",
	StatusApprovedPendingMerge: ":white_check_mark: 승인됨 — 머지해 주세요",
	StatusHasConflicts:         ":warning: 충돌 해결이 필요합니다",
	StatusBlockingDiscussions:  ":no_entry_sign: 미해결 블로킹 디스커션이 있습니다",
}

// resolveNotificationTargets takes classified MRs and a user mapping table,
// and produces a Notification for each MR with resolved Slack targets.
func resolveNotificationTargets(classified []*ClassifiedMR, mapping []UserMappingEntry) []*Notification {
	// Build lookup map: gitlab_username -> slack_id
	slackMap := make(map[string]string)
	for _, m := range mapping {
		slackMap[m.GitLabUsername] = m.SlackID
	}

	var notifications []*Notification
	for _, cmr := range classified {
		var targets []NotificationTarget
		// 같은 사용자가 작성자이자 리뷰어인 경우 두 번 추가되어
		// 멘션이 중복되고 DM에 같은 MR이 두 번 나열됐다.
		seen := make(map[string]struct{})
		addTarget := func(slackID, username string, role TargetRole) {
			if _, dup := seen[slackID]; dup {
				return
			}
			seen[slackID] = struct{}{}
			targets = append(targets, NotificationTarget{
				SlackID:        slackID,
				GitLabUsername: username,
				Role:           role,
			})
		}

		for _, role := range cmr.TargetRoles {
			switch role {
			case RoleAuthor:
				username := authorUsername(cmr.MR.MergeRequest)
				if username == "" {
					log.Printf("warning: MR %s has no resolvable author; skipping author notification",
						cmr.MR.MergeRequest.WebURL)
					continue
				}
				if slackID, ok := slackMap[username]; ok {
					addTarget(slackID, username, RoleAuthor)
				} else if len(mapping) > 0 {
					log.Printf("warning: no Slack mapping for GitLab user %q", username)
				}
			case RoleReviewer:
				for _, reviewer := range cmr.MR.MergeRequest.Reviewers {
					if slackID, ok := slackMap[reviewer.Username]; ok {
						addTarget(slackID, reviewer.Username, RoleReviewer)
					} else if len(mapping) > 0 {
						log.Printf("warning: no Slack mapping for GitLab user %q", reviewer.Username)
					}
				}
			}
		}

		msg := statusMessages[cmr.Status]
		mrInfo := fmt.Sprintf("<%s|%s> (by %s)",
			cmr.MR.MergeRequest.WebURL,
			cmr.MR.MergeRequest.Title,
			authorName(cmr.MR.MergeRequest),
		)

		notifications = append(notifications, &Notification{
			MR:      cmr,
			Targets: targets,
			Message: fmt.Sprintf("%s\n%s", msg, mrInfo),
		})
	}

	return notifications
}

// formatMentions produces a space-separated string of Slack user mentions
// from the given notification targets.
func formatMentions(targets []NotificationTarget) string {
	var mentions []string
	for _, t := range targets {
		mentions = append(mentions, fmt.Sprintf("<@%s>", t.SlackID))
	}
	return strings.Join(mentions, " ")
}
