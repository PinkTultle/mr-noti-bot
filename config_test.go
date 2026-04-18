package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockEnv struct {
	values map[string]string
}

func (e *MockEnv) Getenv(key string) string {
	return e.values[key]
}

func TestLoadConfig(t *testing.T) {
	// Test default values and required environment variables
	t.Run("missing required env variables", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"CONFIG_PATH": "NONEXISTING.yaml",
		}}
		_, err := loadConfig(env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GITLAB_TOKEN environment variable is required")
	})

	t.Run("default GitLab URL", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"GITLAB_TOKEN":      "token",
			"SLACK_WEBHOOK_URL": "webhook",
			"CONFIG_PATH":       "NONEXISTING.yaml",
			"PROJECTS":          "1,2,3",
		}}

		config, err := loadConfig(env)
		assert.NoError(t, err)
		assert.Equal(t, "https://gitlab.com", config.GitLab.URL)
	})

	// Test overriding default values with environment variables
	t.Run("env variables overriding defaults", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"GITLAB_URL":        "https://gitlab.example.com",
			"GITLAB_TOKEN":      "token",
			"SLACK_WEBHOOK_URL": "webhook",
			"CONFIG_PATH":       "NONEXISTING.yaml",
			"PROJECTS":          "1,2,3",
			"CRON_SCHEDULE":     "0 1 * * *",
			"AUTHORS":           "1,username,123",
		}}

		config, err := loadConfig(env)
		assert.NoError(t, err)
		assert.Equal(t, "https://gitlab.example.com", config.GitLab.URL)
		assert.Equal(t, []ConfigProject{
			{ID: 1},
			{ID: 2},
			{ID: 3},
		}, config.Projects)
		assert.Equal(t, "0 1 * * *", config.CronSchedule)
		assert.Equal(t, []ConfigAuthor{
			{ID: 1},
			{Username: "username"},
			{ID: 123},
		}, config.Authors)
	})

	// Test notification-related environment variables
	t.Run("notification env variables", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"GITLAB_TOKEN":             "token",
			"SLACK_WEBHOOK_URL":        "webhook",
			"CONFIG_PATH":              "NONEXISTING.yaml",
			"PROJECTS":                 "1",
			"NOTIFICATION_MODE":        "dm",
			"NOTIFICATION_WEBHOOK_URL": "https://hooks.slack.com/test",
			"NOTIFICATION_BOT_TOKEN":   "xoxb-test-token",
			"STATE_PATH":               "/data/state.json",
		}}
		config, err := loadConfig(env)
		assert.NoError(t, err)
		require.NotNil(t, config.Notification)
		assert.Equal(t, "dm", config.Notification.Mode)
		require.NotNil(t, config.Notification.Webhook)
		assert.Equal(t, "https://hooks.slack.com/test", config.Notification.Webhook.URL)
		require.NotNil(t, config.Notification.Bot)
		assert.Equal(t, "xoxb-test-token", config.Notification.Bot.Token)
		require.NotNil(t, config.State)
		assert.Equal(t, "/data/state.json", config.State.Path)
	})

	// Test summary-related environment variables
	t.Run("summary env variables", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"GITLAB_TOKEN":        "token",
			"SLACK_WEBHOOK_URL":   "webhook",
			"CONFIG_PATH":         "NONEXISTING.yaml",
			"PROJECTS":            "1",
			"SUMMARY_SCHEDULE":    "0 9 * * 1-5",
			"SUMMARY_WEBHOOK_URL": "https://hooks.slack.com/summary",
			"SUMMARY_STALE_DAYS":  "14",
		}}
		config, err := loadConfig(env)
		assert.NoError(t, err)
		require.NotNil(t, config.Summary)
		assert.Equal(t, "0 9 * * 1-5", config.Summary.Schedule)
		assert.Equal(t, "https://hooks.slack.com/summary", config.Summary.WebhookURL)
		assert.Equal(t, 14, config.Summary.StaleDays)
	})

	t.Run("SUMMARY_STALE_DAYS parsing error", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"GITLAB_TOKEN":       "token",
			"SLACK_WEBHOOK_URL":  "webhook",
			"CONFIG_PATH":        "NONEXISTING.yaml",
			"PROJECTS":           "1",
			"SUMMARY_STALE_DAYS": "abc",
		}}
		_, err := loadConfig(env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SUMMARY_STALE_DAYS")
	})

	// Test loading config from file
	t.Run("loading from config file", func(t *testing.T) {
		env := &MockEnv{values: map[string]string{
			"CONFIG_PATH": "config.test.yaml",
		}}

		config, err := loadConfig(env)
		assert.NoError(t, err)

		assert.Equal(t, "https://gitlab.example.com", config.GitLab.URL)
		assert.Equal(t, "abcdef1234567890", config.GitLab.Token)
		assert.Equal(t, "https://hooks.slack.com/services/your-slack-webhook-url", config.Slack.WebhookURL)
		assert.Equal(t, []ConfigProject{
			{ID: 123},
			{ID: 456},
		}, config.Projects)
		assert.Equal(t, []ConfigGroup{
			{ID: 1},
			{ID: 2},
		}, config.Groups)
		assert.Equal(t, "0 7,13 * * 1-5", config.CronSchedule)
		assert.Equal(t, []ConfigAuthor{
			{Username: "janedoe"},
			{Username: "johndoe"},
			{ID: 918},
		}, config.Authors)
	})
}
