package main

import (
	"fmt"
	"log"
	"strings"
)

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

		for _, role := range cmr.TargetRoles {
			switch role {
			case RoleAuthor:
				username := cmr.MR.MergeRequest.Author.Username
				if slackID, ok := slackMap[username]; ok {
					targets = append(targets, NotificationTarget{
						SlackID:        slackID,
						GitLabUsername: username,
						Role:           RoleAuthor,
					})
				} else if len(mapping) > 0 {
					log.Printf("warning: no Slack mapping for GitLab user %q", username)
				}
			case RoleReviewer:
				for _, reviewer := range cmr.MR.MergeRequest.Reviewers {
					if slackID, ok := slackMap[reviewer.Username]; ok {
						targets = append(targets, NotificationTarget{
							SlackID:        slackID,
							GitLabUsername: reviewer.Username,
							Role:           RoleReviewer,
						})
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
			cmr.MR.MergeRequest.Author.Name,
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
