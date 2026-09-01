# slack.go

## 모듈 목적

Slack 전송 계층. `Notifier` 인터페이스를 통해 `WebhookNotifier`,
`DMNotifier`, `LegacyNotifier` 세 가지 전송 방식을 제공한다. 설정에 따라
팩토리 함수(`newNotifier`)가 적합한 구현체를 선택한다.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `SlackClient` | 기존 webhook 전송 인터페이스 (하위 호환) |
| `Notifier` | 알림 전송 인터페이스 (`Send([]*Notification) error`) |
| `WebhookNotifier` | Incoming Webhook으로 상태별 그룹핑 + 멘션 전송, 40건 단위 청크 분할 |
| `DMNotifier` | Slack Bot API로 대상별 개인 DM 전송, 부분 실패를 error로 반환 |
| `LegacyNotifier` | 기존 `SlackClient`를 통한 단순 텍스트 전송 |
| `SlackAPI` | Slack Bot API 래퍼 인터페이스 (테스트용 주입 포인트) |

## 주요 함수

### `newNotifier(config) -> (Notifier, error)`

설정 기반 Notifier 팩토리. 예전에는 `Notifier`만 반환했으나, 지금은 선택된
모드에 필요한 하위 설정이 없을 때 스케줄 첫 tick에서 패닉하는 대신 생성
시점에 에러를 반환한다:

- `config.Notification == nil` → `LegacyNotifier`(기존 `slack.webhook_url` 사용). 이 경로는 실패하지 않는다.
- `mode: "dm"` → `config.Notification.Bot`/`Bot.Token`이 없으면 에러. 있으면 `DMNotifier`.
- `mode: "webhook"`(기본값) → `config.Notification.Webhook`/`Webhook.URL`이 없으면 에러. 있으면 `WebhookNotifier`.

호출부(`main.go`의 `executeSmartNotification`)는 이 에러를 그대로
`execute()`까지 전파해 잘못된 설정으로 스케줄이 조용히 실패하는 것을 막는다.

### `WebhookNotifier.Send` / `buildWebhookMessages`

`Send`는 `client`(`SlackClient`)가 비어 있으면(`nil`) 그 자리에서
`w.webhookURL`로 지연 생성한다 — 테스트에서는 `client`에 mock을 주입해
실제 HTTP 호출 없이 검증한다.

메시지 본문은 `buildWebhookMessages`가 만든다. `summary.go`의
`summaryStatusOrder`로 상태별 그룹핑한 뒤, `summary.go`의
`maxMRsPerMessage`(40) 단위로 잘라 여러 Slack 메시지로 나눈다. Slack
webhook payload는 약 40,000자 제한이 있고, 첫 실행 시 열린 MR 전체가
"변경됨"으로 잡히므로 청크 분할 없이는 메시지 하나가 그 한도를 넘을 수
있다. 청크 경계에서 상태 헤더만 남고 항목이 없는 상황을 피하기 위해,
헤더는 그 상태의 첫 항목이 실제로 쓰일 때만 추가한다.

`Send`는 메시지를 순서대로 `PostWebhook`하며, 중간에 실패하면 몇 번째
메시지(`i+1/len(messages)`)에서 실패했는지 감싸 즉시 반환한다 — 뒤 메시지는
보내지 않는다.

### `formatNotificationLine(n) -> string`

`Notification` 하나를 `<url|제목> (by 작성자)` 형식의 한 줄로 렌더링한다.
`WebhookNotifier`(청크 본문)와 `DMNotifier`(DM 본문)가 공유한다.

### `groupNotificationsByStatus` / `groupNotificationsByTarget`

- `groupNotificationsByStatus`: 상태별로 묶어 `WebhookNotifier`가 상태별
  섹션으로 출력할 수 있게 한다.
- `groupNotificationsByTarget`: Slack ID별로 묶어 `DMNotifier`가 1인당 1개
  DM을 보낼 수 있게 한다. 대상 목록 자체의 중복 제거는 `notification.go`의
  `resolveNotificationTargets`가 이미 처리하므로, 여기서는 단순히
  `Targets`를 순회하며 재구성한다.

### `DMNotifier.Send`

대상별로 하나의 DM으로 묶어 전송한다. **한 대상에 대한 전송 실패가 나머지
전송을 막지는 않지만, 실패가 있었다면 반드시 error를 반환한다.** 여기서
`nil`을 반환하면 호출부(`executeSmartNotification`)가 그대로 `saveState`를
진행해, 실패한 알림의 MR도 "알림 완료" 상태로 기록되어 중복 제거 로직에
걸려 영구적으로 재시도되지 않는다. 실패한 Slack ID 목록은 로그와 반환
에러 메시지 양쪽에 남는다.

### `LegacyNotifier.Send`

`Notification.Message`를 이어붙여 `sendSlackMessage`로 한 번에 전송한다.
청킹이나 대상 분리가 없는 가장 단순한 경로.

## 의존 관계

- `notification.go`: `Notification`, `NotificationTarget`, `formatMentions()`, `statusMessages`
- `classify.go`: `MRStatus`, status 상수들
- `config.go`: `Config`, `NotificationConfig`, `WebhookConfig`, `BotConfig`
- `summary.go`: `maxMRsPerMessage`, `summaryStatusOrder` (정의는 `summary.go`에 있고 여기서 재사용)
- `gitlab.go`: `MergeRequestWithApprovals` (Notification 체인에서 간접 참조)
- 외부: `github.com/slack-go/slack`

## 데이터 흐름

```
resolveNotificationTargets() -> []*Notification
                                     |
                                     v
                       newNotifier(config) -> (Notifier, error)
                                     |
               +---------------------+---------------------+
               |                     |                     |
               v                     v                     v
      WebhookNotifier          DMNotifier           LegacyNotifier
   (groupByStatus, 40건 청크)  (groupByTarget)         (plain text)
               |                     |                     |
               v                     v                     v
    slack.PostWebhook() x N   SlackAPI.PostMessage()  SlackClient.PostWebhook()
    (실패 시 즉시 중단)        (부분 실패 error 반환)
```

## 주의사항

- `WebhookNotifier`와 `DMNotifier`는 부분 실패를 다루는 방식이 다르다:
  전자는 첫 실패에서 중단(뒤 청크는 전송하지 않음), 후자는 끝까지 시도한
  뒤 실패 목록을 모아 error로 반환한다. 어느 쪽이든 `Send`가 error를
  반환하면 `executeSmartNotification`은 `saveState`를 건너뛰어 다음 실행에서
  재시도되게 한다.
