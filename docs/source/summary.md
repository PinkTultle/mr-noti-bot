# summary.go -- 정기 MR 현황 요약

## 모듈 목적

정해진 스케줄마다 현재 열려 있는 MR 전체를 상태별로 묶어 Slack에 한 번에
보고한다. 상태가 바뀐 MR만 개별 알림하는 smart-notification(`main.go`의
`executeSmartNotification`)과 서로 보완하는 관계다: smart-notification은
"무엇이 바뀌었는가"를, summary는 "지금 전체가 어떤가"를 답한다.

## smart-notification과의 관계

- smart-notification: 상태가 바뀐 MR만, 대상자에게(또는 채널에) 개별 알림.
  `state.go`의 상태 파일로 중복 전송을 막는다.
- summary: 열린 MR 전체를 매번 다시 계산해 상태별로 묶어 채널에 보고한다.
  **상태 파일을 전혀 쓰지 않는다** — 매 실행이 독립적인 "현재 스냅샷"이어야
  하기 때문이다. 상태 기반 중복 제거를 걸면, 오래 방치된 MR은 상태가 바뀌지
  않으므로 다시는 보고되지 않는 문제가 생긴다. 대신 `isStale`로 오래된 MR을
  시각적으로 표시해 방치를 드러낸다.
- `main.go`의 `runScheduler`가 `executeJob`(smart-notification)과
  `summaryJob`(summary)을 하나의 스케줄러에 독립적으로 등록한다. 두 스케줄은
  따로 켜고 끌 수 있다.

## 주요 함수

### `resolveSummaryWebhookURL(config) -> string`

웹훅 URL 결정 우선순위: `summary.webhook_url` → `notification.webhook.url` →
빈 문자열. summary 전용 webhook을 따로 지정하지 않으면 smart-notification의
webhook 채널을 그대로 재사용한다. 요약을 더 공개적인/다른 채널로 보내고
싶을 때만 `summary.webhook_url`을 별도로 설정하면 된다.

### `resolveStaleDays(config) -> int`

`summary.stale_days`가 양수로 설정돼 있으면 그 값, 아니면 기본값 7일.

### `isStale(mr, staleDays, now) -> bool`

MR 생성일로부터 `staleDays`일 이상 지났는지 판단한다. `formatMRLine`에서
stale MR 앞에 `:exclamation:` 표시를 붙이는 용도로만 쓰인다 — stale이어도
목록에서 제외되거나 별도 그룹으로 묶이지는 않는다.

### `formatSummaryMessages(classified, staleDays, now) -> []string`

출력을 만드는 핵심 함수. `classify.go`의 분류 결과를 `summaryStatusOrder`
순서로 상태별 그룹핑한 뒤, `maxMRsPerMessage`(40) 단위로 청크를 나눠 여러
Slack 메시지를 만든다. 한 상태 그룹이 40건을 넘으면 그 상태 자체도 여러
메시지로 쪼개지며, 쪼개진 각 메시지에도 상태 헤더가 반복해서 붙는다. 모든
메시지 앞에는 `formatSummaryHeader`가 만든 상단 배너(총 MR 수, 시각)가
다시 붙는다.

### `executeSummary(config) -> error`

운영 진입점. `newGitLabClient`로 GitLab 클라이언트를 만들고, webhook URL이
없으면 즉시 에러를 반환한 뒤(`resolveSummaryWebhookURL` 결과가 빈 문자열),
`executeSummaryWithClient`로 위임한다.

### `executeSummaryWithClient(config, glClient, sc, now) -> error`

테스트 및 실제 실행이 함께 쓰는 주입 지점. `fetchOpenedMergeRequests` →
`filterMergeRequestsByAuthor`(`main.go`의 author 필터를 그대로 재사용) →
`classifyMergeRequests` → `formatSummaryMessages` → 메시지를 순서대로
`PostWebhook`. `now`를 인자로 받기 때문에 "N일 전" 표기와 stale 판정을
테스트에서 고정된 시각으로 검증할 수 있다.

## 의존 관계

- `gitlab.go`: `newGitLabClient`, `fetchOpenedMergeRequests`, `GitLabClient`
- `main.go`: `filterMergeRequestsByAuthor`(author 필터 재사용). `executeSummary`는
  `registerSummaryJob`(스탠드얼론 스케줄러)과 `runLambdaJob`의
  `jobSummary` 분기에서 호출된다.
- `classify.go`: `classifyMergeRequests`, `ClassifiedMR`, `MRStatus`
- `slack.go`: `SlackClient`, `slackClient`. `slack.go`의 `buildWebhookMessages`도
  이 파일에 정의된 `maxMRsPerMessage`, `summaryStatusOrder`를 그대로 재사용한다.
- `config.go`: `Config`, `SummaryConfig`

## 주의사항

- **상태 파일을 쓰지 않는다.** 매 실행이 독립적인 전체 스냅샷이며 재실행해도
  안전(idempotent)하다. 다만 스케줄이 잦으면 그만큼 자주 전체 목록을 다시
  보고하게 된다.
- one-shot 모드(`main.go`의 `shouldRunOneShot`)에서는 summary가 절대 실행되지
  않는다. `summary.schedule`이 설정되는 순간 스케줄러 모드로 강제 전환된다.
- Lambda 환경에서는 `LambdaInput.Job == "summary"`일 때만 실행된다
  (`main.go` 참고). EventBridge 규칙에서 `{"job":"summary"}`로 트리거해야 한다.
- `maxMRsPerMessage`(40)와 `summaryStatusOrder`는 이 파일에 정의돼 있지만
  `slack.go`의 webhook 알림 청킹에도 공유되는 전역 값이다. 값을 바꾸면
  smart-notification의 webhook 청크 크기도 함께 바뀐다.
