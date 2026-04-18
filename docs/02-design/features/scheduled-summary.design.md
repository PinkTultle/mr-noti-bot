# Design: 특정 시간 MR 현황 요약 전송

> Plan 참조: `docs/01-plan/features/scheduled-summary.plan.md`

---

## 1. 설계 개요

`summary_schedule` cron 트리거에 의해 실행되며, 열린 MR을 상태별로 그룹핑한 요약 메시지를 Slack Webhook으로 전송한다.
smart-notification과 **독립된 실행 경로**로 동작하며, 중복 방지 상태 파일은 건드리지 않는다.

### 목표 아키텍처

```
runScheduler(config)
  ├─ executeJob (cron_schedule)     ← 기존 smart-notification
  │    → execute() → executeSmartNotification() 또는 executeLegacy()
  │
  └─ summaryJob (summary_schedule)  ← 신규
       → executeSummary(config)
           → fetchOpenedMergeRequests()     [재활용]
           → classifyMergeRequests()         [재활용]
           → formatSummaryMessage()           [신규 - summary.go]
           → sendSummaryToWebhook()           [신규 - summary.go]
```

### Plan 미확정 사항 확정

Plan Section 9의 3가지 미확정 사항을 Design 단계에서 결정:

| 사항 | 결정 | 이유 |
|------|------|------|
| **요약 전송 대상** | `summary.webhook_url` 독립 설정 지원. 미설정 시 `notification.webhook.url` 폴백 | 요약 채널을 알림 채널과 분리하려는 팀 사용 사례 지원 |
| **오래된 MR 강조 기준** | 기본 7일, `summary.stale_days`로 설정 가능 | 주간 리뷰 사이클 기준. 설정으로 커스터마이즈 허용 |
| **메시지 최대 길이** | MR 40개 초과 시 다중 메시지로 분할 전송 | Slack Webhook 메시지는 40KB 제한, MR당 ~500자 가정 |

---

## 2. 설정 확장 — `config.go`

### 2.1 타입 추가

```go
type SummaryConfig struct {
    Schedule   string `yaml:"schedule"`     // cron expression
    WebhookURL string `yaml:"webhook_url"`  // optional: falls back to notification.webhook.url
    StaleDays  int    `yaml:"stale_days"`   // optional: default 7
}
```

### 2.2 Config 구조체 확장

```go
type Config struct {
    // ... 기존 필드
    Summary *SummaryConfig `yaml:"summary"`
}
```

### 2.3 환경변수 파싱

`loadConfig()`에 다음 블록 추가 (STATE_PATH 블록 뒤에 배치):

```go
if sumSchedule := env.Getenv("SUMMARY_SCHEDULE"); sumSchedule != "" {
    if config.Summary == nil {
        config.Summary = &SummaryConfig{}
    }
    config.Summary.Schedule = sumSchedule
}

if sumWebhookURL := env.Getenv("SUMMARY_WEBHOOK_URL"); sumWebhookURL != "" {
    if config.Summary == nil {
        config.Summary = &SummaryConfig{}
    }
    config.Summary.WebhookURL = sumWebhookURL
}

if sumStale := env.Getenv("SUMMARY_STALE_DAYS"); sumStale != "" {
    days, err := strconv.Atoi(sumStale)
    if err != nil {
        return nil, fmt.Errorf("error parsing SUMMARY_STALE_DAYS: %v", err)
    }
    if config.Summary == nil {
        config.Summary = &SummaryConfig{}
    }
    config.Summary.StaleDays = days
}
```

### 2.4 기본값 결정 헬퍼

설정 값 확인 후 기본값 적용하는 헬퍼를 `summary.go`에 정의 (Config에는 raw 값 유지):

```go
// summary.go
func resolveSummaryWebhookURL(config *Config) string {
    if config.Summary != nil && config.Summary.WebhookURL != "" {
        return config.Summary.WebhookURL
    }
    if config.Notification != nil && config.Notification.Webhook != nil {
        return config.Notification.Webhook.URL
    }
    return "" // caller handles empty
}

func resolveStaleDays(config *Config) int {
    if config.Summary != nil && config.Summary.StaleDays > 0 {
        return config.Summary.StaleDays
    }
    return 7
}
```

---

## 3. Summary 모듈 — `summary.go` (신규)

### 3.1 핵심 함수

```go
package main

import (
    "fmt"
    "log"
    "sort"
    "strings"
    "time"

    "github.com/slack-go/slack"
)

// executeSummary는 전체 열린 MR을 상태별로 요약하여 전송한다.
// 중복 방지 상태 파일을 건드리지 않는다.
func executeSummary(config *Config) error

// formatSummaryMessages는 분류된 MR 목록을 1개 이상의 Slack 메시지 텍스트로 변환한다.
// MR 수가 많을 경우 여러 메시지로 분할한다.
func formatSummaryMessages(classified []*ClassifiedMR, staleDays int, now time.Time) []string

// groupClassifiedByStatus는 분류된 MR을 상태별로 그룹핑한다.
func groupClassifiedByStatus(classified []*ClassifiedMR) map[MRStatus][]*ClassifiedMR

// formatRelativeTime은 생성 시각을 상대 표시로 변환한다 ("3일 전").
func formatRelativeTime(t time.Time, now time.Time) string

// isStale은 MR이 기준 일수 이상 오래되었는지 판단한다.
func isStale(mr *ClassifiedMR, staleDays int, now time.Time) bool
```

### 3.2 executeSummary 구현

```go
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

    gitlabClient := &gitLabClient{client: glClient}

    mrs, err := fetchOpenedMergeRequests(config, gitlabClient)
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
```

### 3.3 메시지 포맷

**템플릿:**

```
:clipboard: *MR 현황 요약* (2026-04-18 09:00)
열린 MR: 총 {N}개

:warning: *충돌 해결 필요* ({count})
  • <URL|Title> (by Author, 3일 전)
  • :exclamation: <URL|Title> (by Author, 10일 전) ← stale

:no_entry_sign: *미해결 블로킹 디스커션* ({count})
  • ...

:pencil2: *변경 요청됨* ({count})
  • ...

:white_check_mark: *승인됨 — 머지 대기* ({count})
  • ...

:eyes: *리뷰 대기* ({count})
  • ...
```

**상태 표시 순서** (smart-notification과 동일, 긴급도 순):
1. `has_conflicts` (:warning:)
2. `blocking_discussions` (:no_entry_sign:)
3. `changes_requested` (:pencil2:)
4. `approved_pending_merge` (:white_check_mark:)
5. `needs_review` (:eyes:)

**stale 표시**: `staleDays` 초과 시 `:exclamation:` 접두사 추가.

**빈 그룹**: count=0 인 상태는 섹션 생략 (간결성).

### 3.4 formatSummaryMessages 분할 로직

```go
const (
    maxMRsPerMessage = 40
)

func formatSummaryMessages(classified []*ClassifiedMR, staleDays int, now time.Time) []string {
    if len(classified) == 0 {
        return []string{formatEmptySummary(now)}
    }

    total := len(classified)
    grouped := groupClassifiedByStatus(classified)

    // Fixed status order (most urgent first)
    statusOrder := []MRStatus{
        StatusHasConflicts, StatusBlockingDiscussions,
        StatusChangesRequested, StatusApprovedPendingMerge,
        StatusNeedsReview,
    }

    // Flatten with status headers for splitting
    var segments []string
    for _, status := range statusOrder {
        mrs, ok := grouped[status]
        if !ok || len(mrs) == 0 {
            continue
        }
        segments = append(segments, formatStatusSection(status, mrs, staleDays, now))
    }

    // Combine segments, splitting if MR count exceeds limit
    return splitSegmentsByMRCount(segments, classified, maxMRsPerMessage, now, total)
}
```

> **분할 단순화**: 첫 설계에서는 상태 섹션 단위로 분할 (섹션 중간에서 자르지 않음).
> 단일 상태가 40개를 초과하면 해당 상태만 재분할. 구현 시 테스트 주도로 정제.

### 3.5 formatRelativeTime

```go
func formatRelativeTime(t time.Time, now time.Time) string {
    d := now.Sub(t)
    days := int(d.Hours() / 24)
    hours := int(d.Hours())

    switch {
    case days >= 1:
        return fmt.Sprintf("%d일 전", days)
    case hours >= 1:
        return fmt.Sprintf("%d시간 전", hours)
    default:
        return "방금 전"
    }
}
```

---

## 4. 스케줄러 확장 — `main.go`

### 4.1 runScheduler 리팩토링

**현재:**
```go
func runScheduler(config *Config) {
    sched := quartz.NewStdScheduler()
    sched.Start(ctx)

    cronTrigger, _ := quartz.NewCronTrigger(config.CronSchedule)
    executeJob := job.NewFunctionJob(...)
    sched.ScheduleJob(...)

    <-ctx.Done()
}
```

**변경 후:**
```go
func runScheduler(config *Config) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sched := quartz.NewStdScheduler()
    sched.Start(ctx)

    // Job 1: Smart notification (existing)
    if config.CronSchedule != "" {
        if err := registerExecuteJob(sched, config); err != nil {
            log.Fatalf("Error scheduling execute job: %v", err)
        }
        log.Printf("Execute job scheduled: %s", config.CronSchedule)
    }

    // Job 2: Scheduled summary (new)
    if config.Summary != nil && config.Summary.Schedule != "" {
        if err := registerSummaryJob(sched, config); err != nil {
            log.Fatalf("Error scheduling summary job: %v", err)
        }
        log.Printf("Summary job scheduled: %s", config.Summary.Schedule)
    }

    <-ctx.Done()
}

func registerExecuteJob(sched quartz.Scheduler, config *Config) error {
    trigger, err := quartz.NewCronTrigger(config.CronSchedule)
    if err != nil {
        return err
    }
    j := job.NewFunctionJob(func(_ context.Context) (int, error) {
        if err := execute(config); err != nil {
            log.Printf("Error during execute: %v", err)
            return 1, err
        }
        return 0, nil
    })
    return sched.ScheduleJob(quartz.NewJobDetail(j, quartz.NewJobKey("executeJob")), trigger)
}

func registerSummaryJob(sched quartz.Scheduler, config *Config) error {
    trigger, err := quartz.NewCronTrigger(config.Summary.Schedule)
    if err != nil {
        return err
    }
    j := job.NewFunctionJob(func(_ context.Context) (int, error) {
        if err := executeSummary(config); err != nil {
            log.Printf("Error during summary: %v", err)
            return 1, err
        }
        return 0, nil
    })
    return sched.ScheduleJob(quartz.NewJobDetail(j, quartz.NewJobKey("summaryJob")), trigger)
}
```

### 4.2 mainStandalone 수정 — one-shot 모드에서 summary 스케줄 처리

현재 one-shot 모드 판정:
```go
if config.CronSchedule == "" {
    execute(config)  // one-shot
    return
}
```

**변경**: `cron_schedule`이 없어도 `summary_schedule`이 있으면 scheduler 실행.

```go
func mainStandalone() {
    config, err := loadConfig(&OsEnv{})
    if err != nil {
        log.Fatalf("Error loading configuration: %v", err)
    }

    // If neither schedule is set, run once and exit
    if config.CronSchedule == "" && (config.Summary == nil || config.Summary.Schedule == "") {
        log.Printf("Running in one-shot mode")
        if err := execute(config); err != nil {
            log.Fatalf("Error executing: %v", err)
        }
        return
    }

    runScheduler(config)
}
```

---

## 5. 테스트 설계

### 5.1 summary_test.go — 신규

| 테스트 | 설명 |
|--------|------|
| `TestFormatSummaryMessages_Empty` | MR 없을 때 "열린 MR: 총 0개" 메시지 1개 |
| `TestFormatSummaryMessages_SingleStatus` | 모두 같은 상태 — 1개 섹션 |
| `TestFormatSummaryMessages_AllStatuses` | 5개 상태 모두 존재 — 순서대로 섹션 표시 |
| `TestFormatSummaryMessages_EmptyGroups_Skipped` | count=0 섹션은 출력되지 않음 |
| `TestFormatSummaryMessages_StaleHighlight` | staleDays 초과 MR에 :exclamation: 표시 |
| `TestFormatSummaryMessages_SplitByCount` | 40개 초과 시 다중 메시지 반환 |
| `TestGroupClassifiedByStatus` | 상태별 정확히 그룹핑 |
| `TestFormatRelativeTime_Days` | 3일 전 |
| `TestFormatRelativeTime_Hours` | 5시간 전 |
| `TestFormatRelativeTime_JustNow` | 방금 전 |
| `TestIsStale_True` | 10일 전 MR, staleDays=7 → true |
| `TestIsStale_False` | 3일 전 MR, staleDays=7 → false |
| `TestResolveSummaryWebhookURL_Explicit` | summary.webhook_url 지정 시 해당 값 |
| `TestResolveSummaryWebhookURL_Fallback` | summary.webhook_url 없음 + notification.webhook.url 있음 → 폴백 |
| `TestResolveSummaryWebhookURL_Empty` | 둘 다 없음 → 빈 문자열 |
| `TestResolveStaleDays_Explicit` | 14 설정 → 14 |
| `TestResolveStaleDays_Default` | 설정 없음 → 7 |

### 5.2 config_test.go — 추가

| 테스트 | 입력 | 기대 |
|--------|------|------|
| summary 환경변수 | `SUMMARY_SCHEDULE="0 9 * * *"`, `SUMMARY_WEBHOOK_URL="..."`, `SUMMARY_STALE_DAYS="14"` | Config.Summary에 각 값 저장 |
| SUMMARY_STALE_DAYS 파싱 에러 | `SUMMARY_STALE_DAYS="abc"` | 에러 반환 |

### 5.3 main_test.go — 추가 (선택)

`registerSummaryJob`와 `registerExecuteJob`는 내부적으로 quartz를 호출하므로 단위 테스트보다는
`mainStandalone` 분기 로직 테스트 정도로 충분. 현재 `main_test.go`에는 이미 통합 테스트 패턴이
있으므로 새 진입점(one-shot 판정)만 검증.

---

## 6. 설정 파일 변경

### 6.1 config.yaml.example

기존 `state:` 블록 뒤에 추가:

```yaml
# Scheduled summary (optional — enable periodic full MR status report)
summary:
  schedule: "0 9 * * 1-5"          # Weekdays 9 AM
  # webhook_url: "..."             # Optional: separate channel
  # stale_days: 7                  # Optional: default 7
```

---

## 7. 구현 순서

| 순서 | 파일 | 작업 | 의존성 |
|------|------|------|--------|
| 1 | `config.go` | `SummaryConfig` 타입 + 환경변수 파싱 | 없음 |
| 2 | `config_test.go` | summary 환경변수 테스트 | 1 |
| 3 | `summary.go` | 헬퍼: `resolveSummaryWebhookURL`, `resolveStaleDays`, `formatRelativeTime`, `isStale` | 1 |
| 4 | `summary.go` | `groupClassifiedByStatus`, `formatStatusSection`, `formatEmptySummary` | 3 |
| 5 | `summary.go` | `formatSummaryMessages` + 분할 로직 | 4 |
| 6 | `summary_test.go` | 모든 포맷/헬퍼 테스트 17건 | 3~5 |
| 7 | `summary.go` | `executeSummary` (GitLab + classify + format + send) | 5 |
| 8 | `main.go` | `runScheduler` 리팩토링 + `registerSummaryJob` 추가 + one-shot 판정 수정 | 7 |
| 9 | `main_test.go` | one-shot 판정 테스트 (선택) | 8 |
| 10 | `config.yaml.example` | summary 항목 추가 | 없음 |

---

## 8. 범위 외 및 위험

### 8.1 범위 외 (Plan에서 이미 확정)

- Lambda 환경 지원 (내장 cron 의존)
- 주간/월간 통계
- 요약 구독자 선택

### 8.2 위험

| 위험 | 대응 |
|------|------|
| 두 job이 동시 실행 시 GitLab API 호출 중복 | go-quartz는 독립 실행. GitLab API 호출 비용은 허용 범위 (초당 10회 제한 미달) |
| `formatSummaryMessages` 분할 경계 버그 (40개 근처) | 40 경계, 41, 80 경계 테스트 케이스로 커버 |
| stale 판정이 서버 시간대 영향 | `time.Now()`를 인자로 받아 테스트 가능. 실제 동작은 UTC 또는 시스템 TZ 둘 다 동작 |
| `notification` 없이 `summary`만 설정한 경우 | `resolveSummaryWebhookURL` 빈 문자열 반환 → `executeSummary` 에러 로그 후 종료. 스케줄러는 계속 실행 |

---

## 9. 대안 검토

### 9.1 Notifier 재사용 vs 직접 호출

| 방안 | 장점 | 단점 |
|------|------|------|
| A: Notifier 인터페이스 재사용 | 기존 추상화 유지 | `Notifier.Send([]*Notification)` 시그니처 불일치 — 요약은 단일 메시지 문자열 |
| **B: 직접 `slack.PostWebhook` (채택)** | 요약은 일방적 메시지 전송이라 단순 | Notifier와 중복처럼 보임 |

**결정**: B안. 요약은 `*Notification` 객체가 필요 없으므로 Notifier 추상화가 오히려 불필요한 간접화.

### 9.2 one-shot 모드에서 summary만 실행

| 방안 | 장점 | 단점 |
|------|------|------|
| A: one-shot → execute만 | 기존 호환 | summary만 설정한 경우 무시됨 |
| **B: one-shot은 execute만, scheduler는 둘 다 (채택)** | 명확한 분리 | 사용자가 "summary만 한 번 돌려보고 싶음"을 못 함 |

**결정**: B안. one-shot은 디버그/테스트 목적이므로 smart notification 흐름에 집중.
summary는 반드시 스케줄러 모드에서 동작한다.

### 9.3 분할 전략

| 방안 | 장점 | 단점 |
|------|------|------|
| A: 문자 수 기반 분할 | 정확한 Slack 제한 준수 | 메시지 중간에서 잘림 — 가독성 저하 |
| **B: MR 개수 기반 분할 (채택)** | 섹션 단위 유지, 가독성 좋음 | 이론상 40개×500자=20KB (여유) |
| C: 상태 섹션 단위 분할 | 가장 깔끔 | 한 상태가 40개 넘으면 대응 불가 |

**결정**: B안. 40개 경계에서 상태 섹션을 유지하되, 단일 상태가 40개를 초과하면 재분할.
