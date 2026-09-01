package main

import (
	"fmt"
	"log"

	"github.com/slack-go/slack"
)

//go:generate mockery --name SlackClient
type SlackClient interface {
	PostWebhook(payload *slack.WebhookMessage) error
}

type slackClient struct {
	webhookURL string
}

func (c *slackClient) PostWebhook(payload *slack.WebhookMessage) error {
	return slack.PostWebhook(c.webhookURL, payload)
}

func sendSlackMessage(client SlackClient, message string) error {
	msg := slack.WebhookMessage{
		Text: message,
	}
	return client.PostWebhook(&msg)
}

// --- Notifier interface ---

//go:generate mockery --name Notifier
type Notifier interface {
	Send(notifications []*Notification) error
}

// --- WebhookNotifier ---

// WebhookNotifier sends grouped notifications to a Slack channel via
// incoming webhook. Notifications are grouped by MR status with
// per-target mentions.
type WebhookNotifier struct {
	webhookURL string
	client     SlackClient // nil이면 Send에서 지연 생성
}

func (w *WebhookNotifier) Send(notifications []*Notification) error {
	if len(notifications) == 0 {
		return nil
	}
	if w.client == nil {
		w.client = &slackClient{webhookURL: w.webhookURL}
	}

	messages := buildWebhookMessages(notifications)
	for i, msg := range messages {
		if err := w.client.PostWebhook(&slack.WebhookMessage{Text: msg}); err != nil {
			return fmt.Errorf("error sending notification message %d/%d: %w", i+1, len(messages), err)
		}
	}
	return nil
}

// buildWebhookMessages groups notifications by status and splits them into
// chunks of at most maxMRsPerMessage MRs, mirroring the summary path.
// Slack rejects webhook payloads beyond roughly 40,000 characters; on the
// first run every open MR counts as changed, so a single message overflows.
func buildWebhookMessages(notifications []*Notification) []string {
	grouped := groupNotificationsByStatus(notifications)

	var messages []string
	var current string
	var count int

	flush := func() {
		if current != "" {
			messages = append(messages, current)
			current = ""
			count = 0
		}
	}

	for _, status := range summaryStatusOrder {
		notifs, ok := grouped[status]
		if !ok {
			continue
		}

		header := statusMessages[status] + "\n"
		current += header

		for _, n := range notifs {
			if count >= maxMRsPerMessage {
				flush()
				current += header // \uc774\uc5b4\uc9c0\ub294 \uba54\uc2dc\uc9c0\uc5d0\ub3c4 \uc0c1\ud0dc \ud5e4\ub354\ub97c \ub2e4\uc2dc \ub123\ub294\ub2e4
			}
			current += formatNotificationLine(n) + "\n"
			if len(n.Targets) > 0 {
				current += "\u2192 " + formatMentions(n.Targets) + "\n"
			}
			count++
		}
		current += "\n"
	}

	flush()
	return messages
}

// formatNotificationLine renders one MR line for webhook messages.
func formatNotificationLine(n *Notification) string {
	return fmt.Sprintf("<%s|%s> (by %s)",
		n.MR.MR.MergeRequest.WebURL,
		n.MR.MR.MergeRequest.Title,
		authorName(n.MR.MR.MergeRequest))
}

// groupNotificationsByStatus groups notifications by their MR status
// for display in a single webhook message.
func groupNotificationsByStatus(notifications []*Notification) map[MRStatus][]*Notification {
	grouped := make(map[MRStatus][]*Notification)
	for _, n := range notifications {
		grouped[n.MR.Status] = append(grouped[n.MR.Status], n)
	}
	return grouped
}

// --- DMNotifier ---

//go:generate mockery --name SlackAPI
type SlackAPI interface {
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
}

// DMNotifier sends per-user direct messages via the Slack Bot API.
// Multiple notifications for the same target are combined into a
// single DM.
type DMNotifier struct {
	token  string
	client SlackAPI // nil by default; created on first Send
}

func (d *DMNotifier) Send(notifications []*Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	if d.client == nil {
		d.client = slack.New(d.token)
	}

	byTarget := groupNotificationsByTarget(notifications)

	// 한 대상이 실패해도 나머지 전송은 계속하되, 실패는 반드시 반환한다.
	// nil을 반환하면 호출부가 saveState를 진행해 실패한 알림이
	// 중복제거에 걸려 영구 유실된다.
	var failed []string
	for slackID, notifs := range byTarget {
		text := "당신에게 액션이 필요한 MR이 있습니다:\n\n"
		for _, n := range notifs {
			text += fmt.Sprintf("%s\n\u2022 %s\n\n",
				statusMessages[n.MR.Status], formatNotificationLine(n))
		}

		if _, _, err := d.client.PostMessage(slackID, slack.MsgOptionText(text, false)); err != nil {
			log.Printf("error sending DM to %s: %v", slackID, err)
			failed = append(failed, slackID)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to send DM to %d of %d targets: %v",
			len(failed), len(byTarget), failed)
	}
	return nil
}

// groupNotificationsByTarget groups notifications by Slack user ID
// so each user receives a single combined DM.
func groupNotificationsByTarget(notifications []*Notification) map[string][]*Notification {
	grouped := make(map[string][]*Notification)
	for _, n := range notifications {
		for _, t := range n.Targets {
			grouped[t.SlackID] = append(grouped[t.SlackID], n)
		}
	}
	return grouped
}

// --- LegacyNotifier ---

// LegacyNotifier sends plain text summaries via the existing webhook
// path, used when no notification configuration is present.
type LegacyNotifier struct {
	client SlackClient
}

func (l *LegacyNotifier) Send(notifications []*Notification) error {
	if len(notifications) == 0 {
		return nil
	}
	var text string
	for _, n := range notifications {
		text += n.Message + "\n\n"
	}
	return sendSlackMessage(l.client, text)
}

// --- Factory ---

// newNotifier creates the appropriate Notifier based on config.
// Returns an error when the sub-block required by the selected mode is absent,
// instead of panicking at the first scheduled tick.
func newNotifier(config *Config) (Notifier, error) {
	if config.Notification == nil {
		return &LegacyNotifier{client: &slackClient{webhookURL: config.Slack.WebhookURL}}, nil
	}
	switch config.Notification.Mode {
	case "dm":
		if config.Notification.Bot == nil || config.Notification.Bot.Token == "" {
			return nil, fmt.Errorf("notification mode is %q but bot token is not configured", "dm")
		}
		return &DMNotifier{token: config.Notification.Bot.Token}, nil
	default:
		if config.Notification.Webhook == nil || config.Notification.Webhook.URL == "" {
			return nil, fmt.Errorf("notification mode is %q but webhook url is not configured", config.Notification.Mode)
		}
		return &WebhookNotifier{webhookURL: config.Notification.Webhook.URL}, nil
	}
}
