package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/xanzy/go-gitlab"
)

// maxMRsPerMessage bounds the number of MRs packed into a single Slack summary
// message. Slack webhook payloads are capped around 40KB; assuming ~500 bytes
// per MR line leaves a healthy margin at 40.
const maxMRsPerMessage = 40

// summaryStatusOrder lists MR statuses in descending urgency so the report
// surfaces the most blocking items first.
var summaryStatusOrder = []MRStatus{
	StatusHasConflicts,
	StatusBlockingDiscussions,
	StatusChangesRequested,
	StatusApprovedPendingMerge,
	StatusNeedsReview,
}

// summaryStatusHeaders maps each MR status to its Slack section header.
var summaryStatusHeaders = map[MRStatus]string{
	StatusHasConflicts:         ":warning: *충돌 해결 필요*",
	StatusBlockingDiscussions:  ":no_entry_sign: *미해결 블로킹 디스커션*",
	StatusChangesRequested:     ":pencil2: *변경 요청됨*",
	StatusApprovedPendingMerge: ":white_check_mark: *승인됨 — 머지 대기*",
	StatusNeedsReview:          ":eyes: *리뷰 대기*",
}

// resolveSummaryWebhookURL returns the webhook URL to use for summary posts.
// Precedence: summary.webhook_url → notification.webhook.url → "".
func resolveSummaryWebhookURL(config *Config) string {
	if config.Summary != nil && config.Summary.WebhookURL != "" {
		return config.Summary.WebhookURL
	}
	if config.Notification != nil && config.Notification.Webhook != nil {
		return config.Notification.Webhook.URL
	}
	return ""
}

// resolveStaleDays returns the configured stale threshold, defaulting to 7.
func resolveStaleDays(config *Config) int {
	if config.Summary != nil && config.Summary.StaleDays > 0 {
		return config.Summary.StaleDays
	}
	return 7
}

// formatRelativeTime renders the age of t as a short Korean phrase.
func formatRelativeTime(t time.Time, now time.Time) string {
	d := now.Sub(t)
	hours := int(d.Hours())
	days := hours / 24

	switch {
	case days >= 1:
		return fmt.Sprintf("%d일 전", days)
	case hours >= 1:
		return fmt.Sprintf("%d시간 전", hours)
	default:
		return "방금 전"
	}
}

// isStale reports whether the MR has been open at least staleDays.
func isStale(mr *ClassifiedMR, staleDays int, now time.Time) bool {
	if mr == nil || mr.MR == nil || mr.MR.MergeRequest == nil {
		return false
	}
	createdAt := mr.MR.MergeRequest.CreatedAt
	if createdAt == nil {
		return false
	}
	return now.Sub(*createdAt) >= time.Duration(staleDays)*24*time.Hour
}

// groupClassifiedByStatus buckets MRs by their classification status.
func groupClassifiedByStatus(classified []*ClassifiedMR) map[MRStatus][]*ClassifiedMR {
	groups := make(map[MRStatus][]*ClassifiedMR)
	for _, mr := range classified {
		if mr == nil {
			continue
		}
		groups[mr.Status] = append(groups[mr.Status], mr)
	}
	return groups
}

// formatMRLine renders one MR bullet, including an exclamation prefix when stale.
func formatMRLine(mr *ClassifiedMR, staleDays int, now time.Time) string {
	prefix := "  • "
	if isStale(mr, staleDays, now) {
		prefix = "  • :exclamation: "
	}

	title := mr.MR.MergeRequest.Title
	url := mr.MR.MergeRequest.WebURL
	author := ""
	if mr.MR.MergeRequest.Author != nil {
		author = mr.MR.MergeRequest.Author.Name
	}

	rel := ""
	if mr.MR.MergeRequest.CreatedAt != nil {
		rel = formatRelativeTime(*mr.MR.MergeRequest.CreatedAt, now)
	}

	if rel != "" {
		return fmt.Sprintf("%s<%s|%s> (by %s, %s)", prefix, url, title, author, rel)
	}
	return fmt.Sprintf("%s<%s|%s> (by %s)", prefix, url, title, author)
}

// formatStatusSection renders a single status section with its header and MR lines.
// Returns empty string when there are no MRs for that status.
func formatStatusSection(status MRStatus, mrs []*ClassifiedMR, staleDays int, now time.Time) string {
	if len(mrs) == 0 {
		return ""
	}

	header, ok := summaryStatusHeaders[status]
	if !ok {
		header = fmt.Sprintf("*%s*", string(status))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%d)\n", header, len(mrs))
	for i, mr := range mrs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatMRLine(mr, staleDays, now))
	}
	return b.String()
}

// formatSummaryHeader produces the top-of-message banner for a summary.
func formatSummaryHeader(totalCount int, now time.Time) string {
	return fmt.Sprintf(":clipboard: *MR 현황 요약* (%s)\n열린 MR: 총 %d개",
		now.Format("2006-01-02 15:04"), totalCount)
}

// formatEmptySummary renders the message used when there are no open MRs.
func formatEmptySummary(now time.Time) string {
	return formatSummaryHeader(0, now)
}

// formatSummaryMessages converts classified MRs into one or more Slack messages.
// When total MR count exceeds maxMRsPerMessage, the output is split while
// keeping status sections intact. A single status that itself exceeds the
// limit is split across messages preserving the section header.
func formatSummaryMessages(classified []*ClassifiedMR, staleDays int, now time.Time) []string {
	total := len(classified)
	if total == 0 {
		return []string{formatEmptySummary(now)}
	}

	grouped := groupClassifiedByStatus(classified)
	header := formatSummaryHeader(total, now)

	// Build a list of (status, chunk) pairs. Oversized status groups are
	// chunked here so downstream packing can treat each chunk atomically.
	type chunk struct {
		status MRStatus
		mrs    []*ClassifiedMR
	}
	var chunks []chunk
	for _, status := range summaryStatusOrder {
		mrs := grouped[status]
		if len(mrs) == 0 {
			continue
		}
		if len(mrs) <= maxMRsPerMessage {
			chunks = append(chunks, chunk{status: status, mrs: mrs})
			continue
		}
		for start := 0; start < len(mrs); start += maxMRsPerMessage {
			end := start + maxMRsPerMessage
			if end > len(mrs) {
				end = len(mrs)
			}
			chunks = append(chunks, chunk{status: status, mrs: mrs[start:end]})
		}
	}

	var messages []string
	var currentSections []string
	currentCount := 0

	flush := func() {
		if len(currentSections) == 0 {
			return
		}
		body := strings.Join(currentSections, "\n\n")
		messages = append(messages, header+"\n\n"+body)
		currentSections = nil
		currentCount = 0
	}

	for _, c := range chunks {
		if currentCount > 0 && currentCount+len(c.mrs) > maxMRsPerMessage {
			flush()
		}
		currentSections = append(currentSections,
			formatStatusSection(c.status, c.mrs, staleDays, now))
		currentCount += len(c.mrs)
	}
	flush()

	if len(messages) == 0 {
		// Should not happen when total > 0, but guard for safety.
		messages = []string{header}
	}
	return messages
}

// executeSummary runs the full summary pipeline: fetch open MRs, classify,
// render message(s), and post to the configured webhook. Independent of
// smart-notification state tracking.
func executeSummary(config *Config) error {
	webhookURL := resolveSummaryWebhookURL(config)
	if webhookURL == "" {
		return fmt.Errorf("no webhook URL configured for summary (set summary.webhook_url or notification.webhook.url)")
	}

	glClient, err := gitlab.NewClient(config.GitLab.Token,
		gitlab.WithBaseURL(config.GitLab.URL))
	if err != nil {
		return fmt.Errorf("error creating GitLab client: %w", err)
	}

	client := &gitLabClient{client: glClient}

	mrs, err := fetchOpenedMergeRequests(config, client)
	if err != nil {
		return fmt.Errorf("error fetching opened merge requests: %w", err)
	}

	classified := classifyMergeRequests(mrs)
	messages := formatSummaryMessages(classified, resolveStaleDays(config), time.Now())

	for i, msg := range messages {
		if err := slack.PostWebhook(webhookURL, &slack.WebhookMessage{Text: msg}); err != nil {
			return fmt.Errorf("error sending summary message %d/%d: %w", i+1, len(messages), err)
		}
	}

	log.Printf("Summary sent: %d MRs in %d message(s)", len(classified), len(messages))
	return nil
}
