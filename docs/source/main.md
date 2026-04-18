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
| `HandleRequest(ctx, input)` | Lambda 핸들러 — `execute(config)` 1회 실행 |
| `runScheduler(config)` | go-quartz 스케줄러에 executeJob/summaryJob 중 설정된 것을 등록하고 ctx.Done 대기 |
| `registerExecuteJob(sched, config)` | smart-notification 트리거 (`cron_schedule`) 등록 |
| `registerSummaryJob(sched, config)` | 요약 전송 트리거 (`summary.schedule`) 등록 — `executeSummary(config)` 호출 |
| `execute(config)` | GitLab MR 조회 후 legacy 또는 smart notification 경로 선택 |
| `executeLegacy(config, mrs)` | 작성자 필터 → 요약 포맷 → Slack Webhook 전송 |
| `executeSmartNotification(config, mrs)` | 분류 → 상태 변경 감지 → 대상자 해석 → Notifier 전송 → 상태 저장 |
| `formatMergeRequestsSummary(mrs)` | legacy 모드의 요약 텍스트 빌드 |
| `filterMergeRequestsByAuthor(mrs, authors)` | 작성자 ID/username 기준 필터 |

## 의존 관계

- `config.go` — `loadConfig`, `Config`, `SummaryConfig`
- `gitlab.go` — `gitLabClient`, `fetchOpenedMergeRequests`
- `classify.go` — `classifyMergeRequests`, `ClassifiedMR`
- `state.go` — `loadState`/`saveState`/`filterChangedMRs`/`buildNewState`
- `notification.go` — `resolveNotificationTargets`, `newNotifier`
- `slack.go` — `slackClient`, `sendSlackMessage`
- `summary.go` — `executeSummary` (summaryJob 콜백)
- `github.com/reugn/go-quartz` — cron 스케줄링
- `github.com/aws/aws-lambda-go/lambda` — Lambda 런타임
- `github.com/xanzy/go-gitlab` — GitLab API 클라이언트

## 주의사항

- **스케줄러 분기 조건**: `cron_schedule` 또는 `summary.schedule` 중 하나라도 설정되어 있으면 `runScheduler`로 진입. 두 스케줄은 독립적으로 켜고 끌 수 있다.
- **one-shot 모드 제약**: one-shot은 `execute(config)`만 호출한다. 요약(summary)은 one-shot에서 실행되지 않으며, 반드시 스케줄러 모드에서 동작한다 (Design 섹션 9.2 결정).
- **Lambda 경로**: `HandleRequest`는 `execute`만 호출하므로 요약은 지원되지 않는다. 요약 기능은 Lambda 환경 외(컨테이너/VM)에서만 사용한다.
- **go-quartz Scheduler 공유**: 두 job은 하나의 `quartz.NewStdScheduler()`에 등록되며 독립적으로 트리거된다. 동시 실행 시 GitLab API 호출이 중복될 수 있으나 허용 범위 내 (Design 섹션 8.2 위험 분석).
- **`log.Fatalf` 사용**: 스케줄 등록 실패 시 프로세스 종료. 잘못된 cron 표현식 같은 설정 오류는 기동 시점에 빠르게 드러낸다.
