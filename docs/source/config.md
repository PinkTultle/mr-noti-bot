# config.go

## 모듈 목적

YAML 파일과 환경 변수에서 애플리케이션 설정을 로드하는 모듈. 환경 변수가 파일 설정을 오버라이드한다.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `Config` | 전체 설정 구조체 (GitLab, Slack, Projects, Groups, Authors, Notification, UserMapping, State) |
| `NotificationConfig` | 알림 모드 설정 (mode: webhook/dm, webhook URL, bot token) |
| `WebhookConfig` | Slack Incoming Webhook URL |
| `BotConfig` | Slack Bot API Token |
| `UserMappingEntry` | GitLab username -> Slack ID 매핑 항목 |
| `StateConfig` | 상태 저장 파일 경로 |
| `Env` | 환경 변수 접근 인터페이스 (테스트 가능성 확보) |

## 주요 함수

### `loadConfig(env Env) (*Config, error)`
설정 로드 순서:
1. CONFIG_PATH (기본 `config.yaml`) 파일에서 YAML 로드
2. 환경 변수로 오버라이드 (PROJECTS, GROUPS, GITLAB_*, SLACK_*, AUTHORS, NOTIFICATION_*, STATE_PATH, CRON_SCHEDULE)
3. 필수 값 검증 (GITLAB_TOKEN, SLACK_WEBHOOK_URL, projects/groups 중 하나)

## 환경 변수 매핑

| 환경 변수 | Config 필드 |
|-----------|------------|
| `NOTIFICATION_MODE` | `Notification.Mode` |
| `NOTIFICATION_WEBHOOK_URL` | `Notification.Webhook.URL` |
| `NOTIFICATION_BOT_TOKEN` | `Notification.Bot.Token` |
| `STATE_PATH` | `State.Path` |

## 의존 관계

- 외부: `gopkg.in/yaml.v2` (YAML 파싱)
- 표준: `os`, `fmt`, `strconv`, `strings`
