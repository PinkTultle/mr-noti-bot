# Plan: 특정 시간 MR 현황 요약 전송

## Executive Summary

| 항목 | 내용 |
|------|------|
| Feature | scheduled-summary |
| 작성일 | 2026-04-18 |
| 예상 규모 | Medium (기능 추가 + 스케줄러 확장) |

### Value Delivered

| 관점 | 내용 |
|------|------|
| **Problem** | smart-notification은 상태 변경 시에만 알림을 보내므로, 전체 MR 현황을 한눈에 파악할 시점이 없다 |
| **Solution** | 별도 스케줄(`summary_schedule`)로 특정 시간에 전체 열린 MR을 상태별로 그룹핑한 요약 리포트를 전송한다 |
| **Function UX Effect** | 매일 아침 스탠드업 시점 등에 팀 전체가 MR 현황을 한눈에 확인한다 |
| **Core Value** | MR 가시성 향상 — 장기 방치 MR 조기 발견, 팀 전체 작업 공유 |

---

## 1. 배경 및 동기

smart-notification은 **이벤트 기반**(상태 변경 시에만 알림)으로 동작한다.
이는 리뷰 지연 방지에는 효과적이지만, 다음과 같은 상황에서는 부족하다:

1. **주기적 현황 파악 부재**: "지금 몇 개의 MR이 열려있고, 어떤 상태인가?"를 알 방법이 없다
2. **스탠드업 용도 부적합**: 매일 아침 팀 전체가 공유해야 할 MR 현황이 없다
3. **장기 방치 MR**: 상태가 변하지 않으면 알림이 오지 않아 조용히 방치될 수 있다

**scheduled-summary**는 smart-notification과 **상호 보완**하는 기능이다.

| 기능 | 트리거 | 내용 | 대상 |
|------|--------|------|------|
| smart-notification | 폴링 (상태 변경 감지) | 변경된 MR 알림 | 해당 작성자/리뷰어 |
| **scheduled-summary** | 지정 시간 (cron) | **전체 열린 MR 현황** | **공용 채널 (전원 공유)** |

## 2. 목표

1. 별도 `summary_schedule` 설정으로 요약 전송 시점 지정
2. 전체 열린 MR을 **상태별로 그룹핑**하여 요약 메시지 생성
3. smart-notification의 `classifyMergeRequests`, `Notifier`, `UserMapping` 재활용
4. 중복 방지 상태 파일에 **영향을 주지 않음** (요약은 항상 전송)
5. 기존 동작 완전 유지 — `summary_schedule` 미설정 시 기능 비활성화

## 3. 현재 구조 (smart-notification 완료 상태)

### 3.1 실행 흐름
```
main() → runScheduler(cron_schedule)
  → executeJob (cron tick 시)
    → execute(config)
       ├─ config.Notification == nil → executeLegacy()
       └─ else → executeSmartNotification()
           → classify → load state → filter changed → resolve targets → send → save state
```

### 3.2 재활용 가능한 컴포넌트

| 컴포넌트 | 위치 | 재활용 방법 |
|---------|------|------------|
| `fetchOpenedMergeRequests` | gitlab.go | MR 조회 — 그대로 사용 |
| `classifyMergeRequests` | classify.go | 상태 분류 — 그대로 사용 |
| `UserMapping` | config.go | GitLab → Slack 매핑 — 그대로 사용 |
| `Notifier` 인터페이스 | slack.go | 전송 추상화 — 요약 전용 구현체 추가 |
| `statusMessages` | notification.go | 상태별 라벨 재활용 |

### 3.3 새로 필요한 것

- **별도 스케줄 트리거**: `summary_schedule` cron을 기존 `cron_schedule`과 동시 실행
- **요약 메시지 포맷터**: 상태별 그룹핑 + 통계 포함
- **요약 전용 실행 경로**: `executeSummary()` — 중복 방지 로직 우회

## 4. 변경 계획

### 4.1 설정 확장

```yaml
# 기존 설정
cron_schedule: "*/10 * * * *"   # 10분마다 smart-notification 폴링

# 신규 설정
summary_schedule: "0 9 * * 1-5"  # 평일 오전 9시 요약 전송

notification:
  mode: "webhook"
  webhook:
    url: "https://hooks.slack.com/..."
# 요약은 반드시 notification 설정이 필요 (전송 대상)
```

**환경변수**: `SUMMARY_SCHEDULE`

### 4.2 스케줄러 확장 (main.go)

`runScheduler`에 두 번째 cron trigger 추가:

```
quartz.NewStdScheduler() 에
  ├─ executeJob (기존) — cron_schedule 트리거
  └─ summaryJob (신규) — summary_schedule 트리거
```

`summary_schedule`이 비어있으면 summaryJob 등록 건너뛰기 (하위 호환).

### 4.3 요약 실행 경로

```go
func executeSummary(config *Config) error {
    // 1. MR 조회 (재활용)
    mrs, err := fetchOpenedMergeRequests(config, gitlabClient)

    // 2. 상태 분류 (재활용)
    classified := classifyMergeRequests(mrs)

    // 3. 요약 메시지 생성 (신규)
    message := formatSummaryMessage(classified)

    // 4. 전송 — Notifier 재활용하되 요약 전용 메서드 또는 직접 Slack 호출
    // ※ 중복 방지 상태(state.json)는 건드리지 않음
}
```

### 4.4 요약 메시지 포맷

```
:clipboard: *MR 현황 요약* (2026-04-18 09:00)

열린 MR: 총 12개

:warning: *충돌 해결 필요* (1)
  • <MR_URL|Title> (by Author, 3일 전)

:no_entry_sign: *미해결 블로킹 디스커션* (2)
  • <MR_URL|Title> (by Author, 5일 전)
  • <MR_URL|Title> (by Author, 1일 전)

:pencil2: *변경 요청됨* (3)
  • ...

:white_check_mark: *승인됨 — 머지 대기* (2)
  • ...

:eyes: *리뷰 대기* (4)
  • ...
```

**특징:**
- 상태별 섹션 + 개수 표시
- 생성일 상대 표시 (`3일 전`, `1일 전`)
- 장기 방치 MR (예: 7일 이상) 시각적 강조 (이모지/볼드)

### 4.5 수정 대상 파일

| 파일 | 변경 내용 |
|------|-----------|
| `config.go` | `SummarySchedule string yaml:"summary_schedule"` 필드 추가 + 환경변수 파싱 |
| `config_test.go` | SUMMARY_SCHEDULE 환경변수 테스트 |
| `summary.go` (신규) | `executeSummary()`, `formatSummaryMessage()`, `groupClassifiedByStatus()` |
| `summary_test.go` (신규) | 포맷팅/그룹핑 테스트 |
| `main.go` | `runScheduler`에 두 번째 cron trigger 추가 |
| `main_test.go` | 요약 스케줄 통합 테스트 |
| `config.yaml.example` | `summary_schedule` 예시 추가 |

## 5. 구현 순서

1. `config.go` — `SummarySchedule` 필드 + `SUMMARY_SCHEDULE` 환경변수
2. `config_test.go` — 설정 로드 테스트
3. `summary.go` — `formatSummaryMessage`, `executeSummary`
4. `summary_test.go` — 포맷팅 테스트 (각 상태 섹션, 빈 MR, 오래된 MR 강조)
5. `main.go` — `runScheduler` 2번째 cron trigger 등록
6. `main_test.go` — 통합 흐름 테스트
7. `config.yaml.example` 갱신

## 6. 범위 외 (Out of Scope)

- 요약 대상 필터 (특정 프로젝트만) — 기존 `projects`/`groups` 범위 사용
- 요약 구독자 선택 (일부 사용자에게만) — 전원 대상 고정
- 주간/월간 리포트 (통계, 머지된 MR 수 등) — 향후 확장
- Lambda 환경 지원 — 내장 cron 의존이므로 standalone만 지원 (smart-notification과 동일)

## 7. 위험 및 고려사항

| 위험 | 대응 |
|------|------|
| 두 cron이 겹치는 시점에 동시 실행 | go-quartz는 각 Job 독립적으로 실행하므로 문제 없음. 단, GitLab API 호출이 중복될 수 있음 (허용 범위) |
| `summary_schedule`만 설정하고 `cron_schedule` 없는 경우 | 허용 — 요약만 받고 이벤트 알림 없는 사용자 고려 |
| 요약이 state.json 건드리지 않음 보장 | `executeSummary`에서 state 함수 호출하지 않음 (코드 리뷰 포인트) |
| DM 모드에서 요약 전송 대상 | UserMapping 전원 대상으로 DM 전송 또는 공용 채널 전용으로 제한 — **Webhook 모드만 지원**으로 단순화 |

## 8. 초기 설계 결정 (Plan 단계에서 확정)

1. **스케줄 분리**: `cron_schedule` (이벤트) + `summary_schedule` (요약) 독립 동작
2. **상태 파일 미사용**: 요약은 중복 방지 우회, 매 스케줄마다 전송
3. **Webhook 모드 전용**: DM 모드에서 요약은 노이즈 증가 우려로 제외. `notification.mode: "dm"`이어도 요약은 webhook URL이 있으면 해당 URL로 전송
4. **메시지 포맷**: 상태별 그룹핑 + 생성일 상대 표시

## 9. 추가 확정 필요 사항 (Design 단계에서 결정)

- 요약 전송 대상: 현재 `notification.webhook.url` 재사용 vs 별도 `summary.webhook.url` 지원 여부
- 오래된 MR 강조 기준 (예: 7일 이상)
- 메시지 최대 길이 제한 (MR 100개 이상 시 분할?)
