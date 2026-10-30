# config.go

## 모듈 목적

YAML 파일과 환경 변수에서 애플리케이션 설정을 로드하는 모듈. 환경 변수가 파일 설정을 오버라이드한다.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `Config` | 전체 설정 구조체 (GitLab, Slack, Projects, Groups, Authors, Notification, UserMapping, State, Summary) |
| `NotificationConfig` | 알림 모드 설정 (mode: webhook/dm, webhook URL, bot token) |
| `WebhookConfig` | Slack Incoming Webhook URL |
| `BotConfig` | Slack Bot API Token |
| `UserMappingEntry` | GitLab username -> Slack ID 매핑 항목 |
| `StateConfig` | 상태 저장 파일 경로 |
| `SummaryConfig` | 정기 요약 설정 (schedule, webhook_url, stale_days) — `summary.go` 참고 |
| `Env` | 환경 변수 접근 인터페이스 (테스트 가능성 확보) |

## 주요 함수

### `loadConfig(env Env) (*Config, error)`
설정 로드 순서:
1. CONFIG_PATH (기본 `config.yaml`) 파일에서 YAML 로드
2. 환경 변수로 오버라이드 (PROJECTS, GROUPS, GITLAB_*, SLACK_*, AUTHORS, NOTIFICATION_*, STATE_PATH, SUMMARY_*, CRON_SCHEDULE)
3. 필수 값 검증 — GITLAB_TOKEN, projects/groups 중 하나, 그리고 아래 두 항목

### 필수 값 검증 상세

**`slack.webhook_url` — legacy 경로가 실제로 실행될 때만 필수.**
`notification` 블록이 없으면 `execute()`는 legacy 경로(`main.go`의
`executeLegacy`)로 가는데, 이 경로가 실제로 호출되는 경우에만
webhook URL을 요구한다(`executeWillRun`로 판정):
- Lambda(`AWS_LAMBDA_RUNTIME_API` 존재)면 스케줄이 EventBridge 규칙에 있어
  호출마다 `execute()`가 실행되므로 무조건 필수.
- 스탠드얼론이면 `cron_schedule`이 설정돼 있거나(스케줄러가 executeJob을
  등록), `shouldRunOneShot(config)`가 true(one-shot 모드는 `execute()`만
  호출)일 때 필수. `summary.schedule`만 설정하고 `cron_schedule`은
  비워둔 구성(요약만 쓰고 smart-notification/legacy는 안 씀)에서는
  `execute()`가 전혀 호출되지 않으므로 이 값이 없어도 통과한다.

**`notification` 블록이 있으면 모드별 필수값을 검증한다:**
- `mode: "dm"` — `notification.bot.token`(또는 `NOTIFICATION_BOT_TOKEN`)이 없으면 에러.
- `mode: "webhook"` 또는 빈 문자열(기본값) — `notification.webhook.url`(또는 `NOTIFICATION_WEBHOOK_URL`)이 없으면 에러.
- 그 외 값 — 알 수 없는 모드로 즉시 거부(`"webhook"` 또는 `"dm"`만 허용).

이 검증은 `slack.go`의 `newNotifier`에도 동일한 로직이 있다 — `loadConfig`는
기동 시점에 빠르게 실패시키기 위한 것이고, `newNotifier`는 설정이
프로그램적으로 구성되는 경로(테스트 등)를 위한 방어선이다.

## 환경 변수 매핑

| 환경 변수 | Config 필드 |
|-----------|------------|
| `GITLAB_URL` | `GitLab.URL` (기본값 `https://gitlab.com`) |
| `GITLAB_TOKEN` | `GitLab.Token` (필수) |
| `SLACK_WEBHOOK_URL` | `Slack.WebhookURL` |
| `PROJECTS` | `Projects` (콤마 구분 ID 목록) |
| `GROUPS` | `Groups` (콤마 구분 ID 목록) |
| `AUTHORS` | `Authors` (콤마 구분, 숫자면 ID로 문자열이면 username으로 파싱) |
| `CRON_SCHEDULE` | `CronSchedule` |
| `NOTIFICATION_MODE` | `Notification.Mode` |
| `NOTIFICATION_WEBHOOK_URL` | `Notification.Webhook.URL` |
| `NOTIFICATION_BOT_TOKEN` | `Notification.Bot.Token` |
| `STATE_PATH` | `State.Path` |
| `SUMMARY_SCHEDULE` | `Summary.Schedule` |
| `SUMMARY_WEBHOOK_URL` | `Summary.WebhookURL` |
| `SUMMARY_STALE_DAYS` | `Summary.StaleDays` (정수 파싱, 실패 시 에러) |

## 의존 관계

- 외부: `gopkg.in/yaml.v2` (YAML 파싱)
- 표준: `os`, `fmt`, `strconv`, `strings`
- `main.go`의 `shouldRunOneShot`을 검증 로직에서 직접 호출한다(순환 의존은
  아님 — 둘 다 `package main`).
