# slack.go

## 모듈 목적

Slack 전송 계층. Notifier 인터페이스를 통해 WebhookNotifier, DMNotifier, LegacyNotifier 세 가지 전송 방식을 제공한다. 설정에 따라 팩토리 함수가 적합한 구현체를 선택한다.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `SlackClient` | 기존 webhook 전송 인터페이스 (하위 호환) |
| `Notifier` | 알림 전송 인터페이스 (`Send([]*Notification) error`) |
| `WebhookNotifier` | Incoming Webhook으로 상태별 그룹핑 + 멘션 전송 |
| `DMNotifier` | Slack Bot API로 대상별 개인 DM 전송 |
| `LegacyNotifier` | 기존 SlackClient를 통한 단순 텍스트 전송 |
| `SlackAPI` | Slack Bot API 래퍼 인터페이스 (테스트용 주입 포인트) |

## 주요 함수

### `newNotifier(config *Config) Notifier`
설정 기반 Notifier 팩토리:
- `config.Notification == nil` -> LegacyNotifier (기존 webhook URL 사용)
- `mode: "dm"` -> DMNotifier (Bot token 사용)
- `mode: "webhook"` (기본) -> WebhookNotifier (알림 전용 webhook URL 사용)

### `groupNotificationsByStatus(notifications) map[MRStatus][]*Notification`
동일 상태의 알림을 묶어 WebhookNotifier가 상태별 섹션으로 출력할 수 있게 한다.

### `groupNotificationsByTarget(notifications) map[string][]*Notification`
동일 대상(Slack ID)의 알림을 묶어 DMNotifier가 1인당 1개 DM을 보낼 수 있게 한다.

## 의존 관계

- `notification.go`: `Notification`, `NotificationTarget`, `formatMentions()`, `statusMessages`
- `classify.go`: `MRStatus`, status 상수들
- `config.go`: `Config`, `NotificationConfig`, `WebhookConfig`, `BotConfig`
- `gitlab.go`: `MergeRequestWithApprovals` (Notification 체인에서 간접 참조)
- 외부: `github.com/slack-go/slack`

## 데이터 흐름

```
resolveNotificationTargets() -> []*Notification
                                     |
                                     v
                          newNotifier(config) -> Notifier
                                     |
               +---------------------+---------------------+
               |                     |                     |
               v                     v                     v
      WebhookNotifier          DMNotifier           LegacyNotifier
      (groupByStatus)        (groupByTarget)         (plain text)
               |                     |                     |
               v                     v                     v
    slack.PostWebhook()    SlackAPI.PostMessage()    SlackClient.PostWebhook()
```
