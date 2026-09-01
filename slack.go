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

	grouped := groupNotificationsByStatus(notifications)
	var text string

	for _, status := range summaryStatusOrder {
		notifs, ok := grouped[status]
		if !ok {
			continue
		}
		text += statusMessages[status] + "\n"
		for _, n := range notifs {
			text += formatNotificationLine(n) + "\n"
			if len(n.Targets) > 0 {
				text += "\u2192 " + formatMentions(n.Targets) + "\n"
			}
		}
		text += "\n"
	}

	return w.client.PostWebhook(&slack.WebhookMessage{Text: text})
}

// formatNotificationLine renders one MR line for webhook messages.
func formatNotificationLine(n *Notification) string {
	return fmt.Sprintf("<%s|%s> (by %s)",
		n.MR.MR.MergeRequest.WebURL,
		n.MR.MR.MergeRequest.Title,
		n.MR.MR.MergeRequest.Author.Name)
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

	for slackID, notifs := range byTarget {
		text := "당신에게 액션이 필요한 MR이 있습니다:\n\n"
		for _, n := range notifs {
			text += fmt.Sprintf("%s\n\u2022 <%s|%s> (by %s)\n\n",
				statusMessages[n.MR.Status],
				n.MR.MR.MergeRequest.WebURL,
				n.MR.MR.MergeRequest.Title,
				n.MR.MR.MergeRequest.Author.Name)
		}

		_, _, err := d.client.PostMessage(slackID, slack.MsgOptionText(text, false))
		if err != nil {
			log.Printf("error sending DM to %s: %v", slackID, err)
			// Continue sending to other targets
		}
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
