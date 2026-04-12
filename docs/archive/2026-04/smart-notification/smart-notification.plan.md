# Plan: MR 상태 기반 스마트 알림

## Executive Summary

| 항목 | 내용 |
|------|------|
| Feature | smart-notification |
| 작성일 | 2026-04-12 |
| 예상 규모 | Large (아키텍처 변경 + 다단계 구현) |

### Value Delivered

| 관점 | 내용 |
|------|------|
| **Problem** | 현재는 열린 MR 목록을 단일 채널에 일괄 전송하여, 누가 어떤 액션을 해야 하는지 알 수 없다 |
| **Solution** | MR 상태를 분류하고, 상태별로 행동해야 할 대상을 결정하여 멘션/DM으로 직접 알린다 |
| **Function UX Effect** | 각 담당자가 자신이 해야 할 일(리뷰, 피드백 반영, 머지)을 Slack에서 즉시 확인한다 |
| **Core Value** | MR 처리 속도 향상 — 리뷰 대기 시간 단축, 머지 지연 방지 |

---

## 1. 배경 및 동기

현재 `mr-noti-bot`은 열린 MR을 조회하여 **단일 Slack 채널에 목록을 전송**한다.
누구에게 어떤 행동을 요청하는 것인지 구분이 없어, 알림을 받아도 **자신이 해야 할 일을 직접 파악**해야 한다.

실제 MR 워크플로우에서는 상태에 따라 행동 주체가 다르다:

| MR 상태 | 행동 주체 | 기대 행동 |
|---------|----------|-----------|
| 리뷰 대기 | 리뷰어 | MR을 리뷰해야 함 |
| 변경 요청됨 | 작성자 | 피드백을 반영해야 함 |
| 승인됨 (머지 대기) | 작성자 | 머지해야 함 |
| 블로킹 디스커션 미해결 | 작성자 + 리뷰어 | 디스커션을 해결해야 함 |

## 2. 목표

1. MR을 상태별로 분류하는 로직 구현
2. 상태에 따라 알림 대상(작성자/리뷰어)을 결정
3. GitLab 사용자 ↔ Slack 사용자 매핑 체계 구축
4. **Webhook 멘션 모드** / **Bot DM 모드** 설정으로 전환 가능
5. 기존 동작(하위 호환성) 유지 — 새 설정 미사용 시 현재와 동일하게 동작

## 3. 현재 구조 분석

### 3.1 현재 실행 흐름
```
execute()
  → fetchOpenedMergeRequests()     ← 모든 열린 MR + 승인 정보
  → filterMergeRequestsByAuthor()  ← 작성자 필터 (선택적)
  → formatMergeRequestsSummary()   ← 단일 텍스트로 포매팅
  → sendSlackMessage()             ← 단일 Webhook 전송
```

### 3.2 사용 가능한 GitLab MR 필드 (go-gitlab)
- `Author *BasicUser` — 작성자
- `Reviewers []*BasicUser` — 리뷰어 목록
- `BlockingDiscussionsResolved bool` — 블로킹 디스커션 해결 여부
- `MergeStatus string` — 머지 가능 상태
- `ApprovedBy` — 승인자 목록 (MergeRequestApprovals API)
- `HasConflicts bool` — 충돌 여부
- `Draft bool` — Draft 여부

### 3.3 변경이 필요한 영역
- **Config**: 알림 모드, 사용자 매핑, Bot 토큰
- **상태 판단**: 새 모듈 — MR → 상태 분류
- **알림 대상 결정**: 상태 × 사용자 매핑 → 대상 목록
- **전송**: Webhook 멘션 / Bot DM 분기

## 4. 목표 아키텍처

```
execute()
  → fetchOpenedMergeRequests()          ← 기존 유지
  → classifyMergeRequests()             ← [신규] 상태 분류
  → loadPreviousState()                 ← [신규] 이전 알림 상태 로드
  → filterChangedMRs()                  ← [신규] 상태 변경된 MR만 추출
  → resolveNotificationTargets()        ← [신규] 대상 결정 + 사용자 매핑
  → sendNotifications()                 ← [신규] 모드별 전송
      ├─ mode: "webhook" → 채널 멘션
      └─ mode: "dm"      → Bot DM 개별 전송
  → saveCurrentState()                  ← [신규] 현재 상태 저장
```

## 5. 설정 구조

```yaml
gitlab:
  url: https://gitlab.com
  token: your-gitlab-token

# 알림 설정
notification:
  mode: "webhook"                # "webhook" | "dm"

  # mode: webhook — 기존 Webhook + 멘션
  webhook:
    url: "https://hooks.slack.com/services/..."

  # mode: dm — Slack Bot API
  bot:
    token: "xoxb-..."

# GitLab ↔ Slack 사용자 매핑
user_mapping:
  - gitlab_username: "janedoe"
    slack_id: "U01234ABCD"
  - gitlab_username: "johndoe"
    slack_id: "U05678EFGH"

# 알림 중복 방지 — 상태 저장 경로
state:
  path: "/data/notification_state.json"   # Docker Volume 마운트 경로

# 기존 설정 (하위 호환)
projects:
  - id: 123
groups:
  - id: 1
cron_schedule: "0 7,13 * * 1-5"
```

### 배포 환경
- **Docker standalone** (go-quartz 내장 cron) 확정
- 상태 파일은 Docker Volume 마운트로 유지 (`-v ./data:/data`)
- Lambda/K8s CronJob 지원은 향후 확장 (StateStore 인터페이스화)

### 하위 호환성
- `notification` 블록 없으면 기존 `slack.webhook_url` 사용 → 현재와 동일 동작
- `user_mapping` 없으면 멘션/DM 불가 → 기존처럼 목록만 전송
- `state.path` 미설정 시 중복 방지 비활성화 → 매 실행마다 전체 알림 (현재 동작)

## 6. MR 상태 분류 규칙

| 상태 | 판단 조건 | 알림 대상 | 메시지 |
|------|----------|-----------|--------|
| `needs_review` | 승인자 0명 + Draft 아님 | 리뷰어 전원 | "리뷰를 기다리고 있습니다" |
| `changes_requested` | 리뷰어 있음 + 승인자 < 리뷰어 수 + 디스커션 미해결 | 작성자 | "변경 요청된 피드백이 있습니다" |
| `approved_pending_merge` | 승인자 ≥ 1명 + 디스커션 모두 해결 | 작성자 | "승인됨 — 머지해 주세요" |
| `has_conflicts` | HasConflicts == true | 작성자 | "충돌 해결이 필요합니다" |
| `blocking_discussions` | BlockingDiscussionsResolved == false | 작성자 + 리뷰어 | "미해결 블로킹 디스커션이 있습니다" |

> 하나의 MR이 여러 상태에 해당할 수 있다 (예: `has_conflicts` + `needs_review`).
> 우선순위: `has_conflicts` > `blocking_discussions` > `changes_requested` > `approved_pending_merge` > `needs_review`

## 7. 구현 스텝 (PDCA 단위)

각 스텝은 독립적인 PDCA 사이클로 진행하되, 이전 스텝에 의존한다.

### Step 1: MR 상태 분류 엔진
- MR → 상태 분류 함수 (`classifyMergeRequest`)
- 입력: `MergeRequestWithApprovals`
- 출력: `MRStatus` (enum + 대상 역할)
- **파일**: `classify.go` (신규)
- **테스트**: 상태별 분류 정확성

### Step 2: 사용자 매핑 + 알림 대상 결정
- Config에 `UserMapping` 추가
- `resolveNotificationTargets()` — 상태 + 매핑 → Slack ID 목록
- 매핑 없는 사용자 처리 (로그 경고, 스킵)
- **파일**: `config.go` (확장), `notification.go` (신규)
- **테스트**: 매핑 정상/누락 케이스

### Step 3: Webhook 멘션 모드
- Config에 `Notification.Mode`, `Notification.Webhook` 추가
- 기존 `sendSlackMessage` → `sendWebhookNotification` 리팩토링
- 상태별 메시지 포맷 + `<@SLACK_ID>` 멘션 삽입
- 기존 설정 하위 호환 유지
- **파일**: `slack.go` (확장), `config.go` (확장)
- **테스트**: 멘션 포함 메시지 생성

### Step 4: Bot DM 모드
- Config에 `Notification.Bot.Token` 추가
- Slack `chat.postMessage` API 클라이언트
- 대상별 개별 DM 전송
- **파일**: `slack.go` (확장), `config.go` (확장)
- **의존**: `slack-go/slack` 라이브러리 (이미 의존 중)
- **테스트**: DM 전송 mock 테스트

### Step 5: 알림 중복 방지 (상태 저장)
- `state.go` (신규) — 알림 상태 저장/로드
- 저장 구조: `{ "MR_IID:ProjectID": { "status": "needs_review", "notifiedAt": "..." } }`
- 실행 시: 이전 상태와 비교 → 상태가 변경된 MR만 알림 전송
- 전송 후: 새 상태를 파일에 기록
- 머지/닫힌 MR은 상태 파일에서 자동 제거 (stale 방지)
- `state.path` 미설정 시 중복 방지 비활성화 (현재 동작 유지)
- **파일**: `state.go` (신규), `state_test.go` (신규)
- **테스트**: 상태 저장/로드, 변경 감지, stale 제거

### Step 6: execute() 통합 + 모드 전환
- `execute()` 흐름을 목표 아키텍처로 리팩토링
- `notification.mode` 설정에 따른 분기
- 상태 비교 → 변경된 MR만 알림 → 상태 갱신
- 설정 미지정 시 기존 동작 (레거시 모드)
- **파일**: `main.go` (리팩토링)
- **테스트**: 통합 테스트 (모드별 E2E 흐름)

```
의존 관계:
Step 1 (상태 분류) ─┐
                    ├→ Step 6 (통합)
Step 2 (사용자 매핑) ┤
                    │
Step 3 (멘션 모드) ──┤
Step 4 (DM 모드) ───┤
Step 5 (중복 방지) ──┘
```

> Step 1, 2는 병렬 진행 가능. Step 3, 4, 5도 병렬 가능. Step 6은 전부 완료 후.

## 8. 수정/생성 대상 파일

| 파일 | 작업 | 스텝 |
|------|------|------|
| `classify.go` (신규) | MR 상태 분류 엔진 | 1 |
| `classify_test.go` (신규) | 분류 테스트 | 1 |
| `notification.go` (신규) | 알림 대상 결정 + 전송 오케스트레이션 | 2, 6 |
| `notification_test.go` (신규) | 알림 대상 결정 테스트 | 2, 6 |
| `state.go` (신규) | 알림 상태 저장/로드/비교 | 5 |
| `state_test.go` (신규) | 상태 저장/변경 감지 테스트 | 5 |
| `config.go` | UserMapping, Notification, State 설정 추가 | 2, 3, 4, 5 |
| `config_test.go` | 새 설정 로드 테스트 | 2, 3, 4, 5 |
| `slack.go` | 멘션 전송 + DM 전송 확장 | 3, 4 |
| `slack_test.go` | 전송 테스트 | 3, 4 |
| `main.go` | execute() 리팩토링 | 6 |
| `main_test.go` | 통합 테스트 | 6 |
| `config.yaml.example` | 새 설정 예시 | 2 |
| `mocks/` | 새 인터페이스 mock 생성 | 3, 4 |

## 9. 범위 외 (Out of Scope)

- 특정 시간에 전체 MR 현황 요약 전송 → **별도 기능 `scheduled-summary`**로 분리
- Slack 인터랙티브 버튼 (머지/승인 버튼)
- GitLab Webhook 이벤트 기반 실시간 알림 (현재는 폴링 방식)
- Lambda/K8s CronJob용 원격 상태 저장소 (DynamoDB 등) — 향후 확장

## 10. 기존 reviewer-filter Plan과의 관계

`docs/01-plan/features/reviewer-filter.plan.md`의 내용은 이 Plan의 **Step 1, 2에 흡수**된다.
- 리뷰어 필드 활용 → Step 1 (상태 분류)에서 `Reviewers` 필드 사용
- 리뷰어 필터링 → Step 2 (알림 대상 결정)에서 리뷰어를 대상으로 지정

reviewer-filter는 독립 기능이 아닌 smart-notification의 하위 스텝으로 통합되었으므로,
별도 PDCA 사이클로 진행하지 않는다.
