# main.go — 진입점 및 스케줄러

## 모듈 목적

프로세스 진입점. 실행 환경(AWS Lambda vs 스탠드얼론)을 감지하고, 스탠드얼론 모드에서는 설정된 스케줄에 따라 one-shot 실행 또는 go-quartz 기반 스케줄러로 분기한다.

## 주요 함수

| 함수 | 역할 |
|------|------|
| `main()` | 환경 감지(`AWS_LAMBDA_RUNTIME_API`) 후 `mainLambda` 또는 `mainStandalone` 호출 |
| `mainStandalone()` | 설정 로드 → `shouldRunOneShot` 판정 → one-shot 실행 또는 `runScheduler` 진입 |
| `shouldRunOneShot(config)` | `cron_schedule`와 `summary.schedule`가 모두 비어있을 때만 true |
| `mainLambda()` | `lambda.Start(HandleRequest)` 호출 |
| `HandleRequest(ctx, input)` | Lambda 핸들러 — `loadConfig` 후 `runLambdaJob(config, input)`로 위임 |
| `runLambdaJob(config, input)` | `input.normalizedJob()`에 따라 `execute` 또는 `executeSummary` 호출. `HandleRequest`에서 분리해 `loadConfig` 없이 단위 테스트 가능 |
| `runScheduler(config)` | go-quartz 스케줄러에 executeJob/summaryJob 중 설정된 것을 등록하고 ctx.Done 대기 |
| `registerExecuteJob(sched, config)` | smart-notification 트리거 (`cron_schedule`) 등록 |
| `registerSummaryJob(sched, config)` | 요약 전송 트리거 (`summary.schedule`) 등록 — `executeSummary(config)` 호출 |
| `execute(config)` | GitLab MR 조회 → author 필터 → legacy 또는 smart notification 경로 선택 |
| `executeLegacy(config, mrs)` | 요약 포맷 → Slack Webhook 전송 (author 필터는 `execute`에서 이미 적용됨) |
| `executeSmartNotification(config, mrs)` | 분류 → 상태 변경 감지 → 대상자 해석 → Notifier 전송 → 상태 저장 |
| `formatMergeRequestsSummary(mrs)` | legacy 모드의 요약 텍스트 빌드 |
| `filterMergeRequestsByAuthor(mrs, authors)` | 작성자 ID/username 기준 필터. `execute`와 `summary.go`의 `executeSummaryWithClient` 양쪽에서 재사용 |

## LambdaInput과 job 선택

```go
type LambdaInput struct {
    Job string `json:"job"`
}
```

EventBridge 등 트리거가 보내는 이벤트의 `job` 필드로 어떤 파이프라인을
실행할지 고른다. `normalizedJob()`은 `Job`이 빈 문자열이면 `"notify"`로
취급한다 — 기존에 빈 이벤트를 보내던 EventBridge 규칙이 계속 알림을
트리거하도록 하기 위한 하위 호환이다. `"summary"`를 지정하면
`executeSummary`가 실행되고, 그 외 알 수 없는 값은 에러로 거부된다.

## 의존 관계

- `config.go` — `loadConfig`, `Config`, `SummaryConfig`
- `gitlab.go` — `newGitLabClient`, `fetchOpenedMergeRequests`
- `classify.go` — `classifyMergeRequests`, `ClassifiedMR`
- `state.go` — `loadState`/`saveState`/`filterChangedMRs`/`buildNewState`/`isStateCorrupted`
- `notification.go` — `resolveNotificationTargets`
- `slack.go` — `slackClient`, `sendSlackMessage`, `newNotifier`
- `summary.go` — `executeSummary` (summaryJob 콜백이자 `jobSummary` 분기)
- `github.com/reugn/go-quartz` — cron 스케줄링
- `github.com/aws/aws-lambda-go/lambda` — Lambda 런타임
- `github.com/xanzy/go-gitlab` — GitLab API 클라이언트

## 주의사항

- **스케줄러 분기 조건**: `cron_schedule` 또는 `summary.schedule` 중 하나라도 설정되어 있으면 `runScheduler`로 진입. 두 스케줄은 독립적으로 켜고 끌 수 있다.
- **one-shot 모드 제약**: one-shot은 `execute(config)`만 호출한다. 요약(summary)은 one-shot에서 실행되지 않으며, 반드시 스케줄러 모드에서 동작한다 (Design 섹션 9.2 결정).
- **Lambda 경로**: `HandleRequest`는 `LambdaInput.Job`에 따라 `execute` 또는 `executeSummary`를 선택한다. 요약을 쓰려면 EventBridge 규칙이 `{"job":"summary"}`를 보내도록 구성해야 한다.
- **go-quartz Scheduler 공유**: 두 job은 하나의 `quartz.NewStdScheduler()`에 등록되며 독립적으로 트리거된다. 동시 실행 시 GitLab API 호출이 중복될 수 있으나 허용 범위 내 (Design 섹션 8.2 위험 분석).
- **`log.Fatalf` 사용**: 스케줄 등록 실패 시 프로세스 종료. 잘못된 cron 표현식 같은 설정 오류는 기동 시점에 빠르게 드러낸다.
- **author 필터는 모든 경로에 적용된다**: `execute(config)`가 `fetchOpenedMergeRequests` 직후, legacy/smart 분기 이전에 `filterMergeRequestsByAuthor`를 호출한다. 예전에는 legacy 경로에서만 걸러, `notification` 블록을 추가하는 순간(smart 모드로 전환되는 순간) authors 설정이 조용히 무시됐다. summary 경로(`summary.go`)도 동일한 필터 함수를 별도로 호출해 일관성을 맞춘다.
- **상태 저장 실패는 양쪽 분기 모두 에러다**: `executeSmartNotification`은 "변경 없음"과 "변경 있음(알림 전송함)" 두 분기 모두에서 `saveState` 실패를 error로 반환한다. 예전에는 변경 없음 분기만 에러였고, 알림을 이미 보낸 분기는 경고 로그로 넘어가 같은 장애가 MR 활동량에 따라 다르게(때로는 조용히) 보고됐다. 알림 전송 후 저장 실패는 다음 실행에서 중복 알림이 발생함을 뜻하므로, 에러 메시지에 그 사실을 명시한다.
