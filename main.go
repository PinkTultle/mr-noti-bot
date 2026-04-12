package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/reugn/go-quartz/job"
	"github.com/reugn/go-quartz/quartz"
	"github.com/xanzy/go-gitlab"
)

func main() {
	// Check if running in AWS Lambda environment
	if _, ok := os.LookupEnv("AWS_LAMBDA_RUNTIME_API"); ok {
		mainLambda()
	} else {
		mainStandalone()
	}
}

// Entry point for normal execution as a standalone application.
func mainStandalone() {
	config, err := loadConfig(&OsEnv{})
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	if config.CronSchedule == "" {
		log.Printf("Running in one-shot mode")
		if err := execute(config); err != nil {
			log.Fatalf("Error executing: %v", err)
		}
		return
	}

	runScheduler(config)
}

// Entry point for AWS Lambda execution.
func mainLambda() {
	lambda.Start(HandleRequest)
}

func runScheduler(config *Config) {
	log.Printf("Running in cron mode with schedule: %s", config.CronSchedule)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := quartz.NewStdScheduler()
	sched.Start(ctx)

	cronTrigger, err := quartz.NewCronTrigger(config.CronSchedule)
	if err != nil {
		log.Fatalf("Error creating cron trigger: %v", err)
	}

	executeJob := job.NewFunctionJob(func(_ context.Context) (int, error) {
		if err := execute(config); err != nil {
			log.Printf("Error during scheduled execution: %v", err)
			return 1, err // Indicate failure
		}
		return 0, nil // Indicate success
	})

	err = sched.ScheduleJob(quartz.NewJobDetail(executeJob, quartz.NewJobKey("executeJob")), cronTrigger)
	if err != nil {
		log.Fatalf("Error scheduling job: %v", err)
	}

	<-ctx.Done()
}

// LambdaInput represents the input event for the Lambda function.
type LambdaInput struct{}

// HandleRequest is the main Lambda handler function.
func HandleRequest(ctx context.Context, input LambdaInput) (string, error) {
	config, err := loadConfig(&OsEnv{})
	if err != nil {
		log.Printf("Error loading configuration: %v", err)
		return "", err
	}

	err = execute(config)
	if err != nil {
		return "", err
	}

	return "Success", nil
}

func execute(config *Config) error {
	glClient, err := gitlab.NewClient(config.GitLab.Token,
		gitlab.WithBaseURL(config.GitLab.URL))
	if err != nil {
		return fmt.Errorf("error creating GitLab client: %w", err)
	}

	gitlabClient := &gitLabClient{client: glClient}

	mrs, err := fetchOpenedMergeRequests(config, gitlabClient)
	if err != nil {
		return fmt.Errorf("error fetching opened merge requests: %w", err)
	}

	// Legacy mode: no notification config → existing behavior
	if config.Notification == nil {
		return executeLegacy(config, mrs)
	}

	// Smart notification mode
	return executeSmartNotification(config, mrs)
}

// executeLegacy preserves the original behavior: filter by author, format summary, send to webhook.
func executeLegacy(config *Config, mrs []*MergeRequestWithApprovals) error {
	mrs = filterMergeRequestsByAuthor(mrs, config.Authors)

	if len(mrs) == 0 {
		log.Println("No opened merge requests found.")
		return nil
	}

	summary := formatMergeRequestsSummary(mrs)

	sc := &slackClient{webhookURL: config.Slack.WebhookURL}
	if err := sendSlackMessage(sc, summary); err != nil {
		return fmt.Errorf("error sending Slack message: %w", err)
	}

	log.Println("Successfully sent merge request summary to Slack.")
	return nil
}

// executeSmartNotification classifies MRs, deduplicates, resolves targets, and sends notifications.
func executeSmartNotification(config *Config, mrs []*MergeRequestWithApprovals) error {
	if len(mrs) == 0 {
		log.Println("No opened merge requests found.")
		return nil
	}

	// 1. Classify MRs by status
	classified := classifyMergeRequests(mrs)

	// 2. Load previous state for deduplication
	statePath := ""
	if config.State != nil {
		statePath = config.State.Path
	}
	prevState, err := loadState(statePath)
	if err != nil {
		log.Printf("Warning: could not load state: %v", err)
		prevState = &NotificationState{Notifications: make(map[string]MRNotificationRecord)}
	}

	// 3. Filter to only MRs whose status changed
	changed := filterChangedMRs(classified, prevState)

	if len(changed) == 0 {
		log.Println("No MR status changes detected.")
		return saveState(statePath, buildNewState(classified))
	}

	// 4. Resolve notification targets via user mapping
	notifications := resolveNotificationTargets(changed, config.UserMapping)

	// 5. Send via configured notifier
	notifier := newNotifier(config)
	if err := notifier.Send(notifications); err != nil {
		return fmt.Errorf("error sending notifications: %w", err)
	}

	// 6. Save current state
	if err := saveState(statePath, buildNewState(classified)); err != nil {
		log.Printf("Warning: could not save state: %v", err)
	}

	log.Printf("Sent %d notifications for %d changed MRs.", len(notifications), len(changed))
	return nil
}

func formatMergeRequestsSummary(mrs []*MergeRequestWithApprovals) string {
	var summary string
	for _, mr := range mrs {
		approvedBy := strings.Join(mr.ApprovedBy, ", ")
		if approvedBy == "" {
			approvedBy = "None"
		}

		createdAtStr := mr.MergeRequest.CreatedAt.Format("2 January 2006, 15:04 MST")

		var extra string
		if !mr.MergeRequest.BlockingDiscussionsResolved {
			extra = ":warning: Has unresolved blocking discussions"
		}

		summary += fmt.Sprintf(
			":arrow_forward: <%s|%s>\n*Author:* %s\n*Created at:* %s\n*Approved by:* %s\n",
			mr.MergeRequest.WebURL, mr.MergeRequest.Title, mr.MergeRequest.Author.Name, createdAtStr, approvedBy,
		)

		if extra != "" {
			summary += fmt.Sprintf("*Extra:* %s\n", extra)
		}

		summary += "\n"
	}

	return summary
}

func filterMergeRequestsByAuthor(mrs []*MergeRequestWithApprovals, authors []ConfigAuthor) []*MergeRequestWithApprovals {
	if len(authors) == 0 {
		return mrs
	}

	var filteredMRs []*MergeRequestWithApprovals
	for _, mr := range mrs {
		for _, user := range authors {
			if (user.ID != 0 && user.ID == mr.MergeRequest.Author.ID) ||
				(user.Username != "" && user.Username == mr.MergeRequest.Author.Username) {
				filteredMRs = append(filteredMRs, mr)
				break
			}
		}
	}
	return filteredMRs
}
