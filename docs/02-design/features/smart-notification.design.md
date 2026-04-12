# Design: MR 상태 기반 스마트 알림

> Plan 참조: `docs/01-plan/features/smart-notification.plan.md`

---

## 1. 설계 개요

MR 상태를 분류하고, 상태별 알림 대상을 결정하여, 설정에 따라 Webhook 멘션 또는 Bot DM으로 전송한다.
중복 알림 방지를 위해 이전 알림 상태를 JSON 파일로 저장하고, 상태가 변경된 MR만 알림한다.

### 목표 아키텍처

```
execute()
  → fetchOpenedMergeRequests()          [기존 유지]
  → classifyMergeRequests()             [Step 1 - classify.go]
  → loadState() / filterChanged()       [Step 5 - state.go]
  → resolveNotificationTargets()        [Step 2 - notification.go]
  → sendNotifications()                 [Step 3,4 - slack.go]
      ├─ mode: "webhook" → 채널 멘션
      └─ mode: "dm"      → Bot DM
  → saveState()                         [Step 5 - state.go]
```

---

## 2. Step 1: MR 상태 분류 엔진 — `classify.go`

### 2.1 타입 정의

```go
// MRStatus는 MR의 현재 워크플로우 상태를 나타낸다.
type MRStatus string

const (
    StatusNeedsReview         MRStatus = "needs_review"
    StatusChangesRequested    MRStatus = "changes_requested"
    StatusApprovedPendingMerge MRStatus = "approved_pending_merge"
    StatusHasConflicts        MRStatus = "has_conflicts"
    StatusBlockingDiscussions MRStatus = "blocking_discussions"
)

// TargetRole은 알림 대상의 역할을 나타낸다.
type TargetRole string

const (
    RoleAuthor   TargetRole = "author"
    RoleReviewer TargetRole = "reviewer"
)

// ClassifiedMR은 상태가 분류된 MR이다.
type ClassifiedMR struct {
    MR          *MergeRequestWithApprovals
    Status      MRStatus
    TargetRoles []TargetRole   // 이 상태에서 알림받아야 할 역할
    ProjectID   int
}
```

### 2.2 분류 함수

```go
func classifyMergeRequest(mr *MergeRequestWithApprovals, projectID int) *ClassifiedMR
```

**분류 로직 (우선순위 순):**

```
1. HasConflicts == true
   → StatusHasConflicts, targets: [author]

2. len(ApprovedBy) > 0 && len(ApprovedBy) < len(Reviewers)
   → StatusChangesRequested, targets: [author]

3. BlockingDiscussionsResolved == false
   → StatusBlockingDiscussions, targets: [author, reviewer]

4. len(ApprovedBy) >= 1 && BlockingDiscussionsResolved
   → StatusApprovedPendingMerge, targets: [author]

5. len(ApprovedBy) == 0
   → StatusNeedsReview, targets: [reviewer]
```

> **우선순위 적용**: 첫 번째 매칭되는 상태를 반환. 한 MR에 하나의 상태만 할당.
> 이유: 충돌이 가장 긴급하므로 최우선 처리하고, 부분 승인(changes_requested)을 블로킹 디스커션보다 먼저 분류한다.
> 원래 설계에서는 blocking_discussions가 changes_requested보다 우선이었으나, 기존 우선순위 2(blocking)가 3(changes_requested)의 `!BlockingDiscussionsResolved` 조건을 먼저 소비하여 changes_requested가 도달 불가능한 문제가 있었다. 구현 시 changes_requested를 우선순위 2로 이동하고 `!BlockingDiscussionsResolved` 조건을 제거하여 해결했다.

**배치 분류:**

```go
func classifyMergeRequests(mrs []*MergeRequestWithApprovals, projectIDs map[int]int) []*ClassifiedMR
```

- `projectIDs`: MR IID → ProjectID 매핑 (fetchOpenedMergeRequests에서 추적 필요)
- Draft MR은 분류에서 제외 (현재 fetchOpenedMergeRequests에서 WIP 이미 제외 중)

### 2.3 ProjectID 추적 문제

현재 `fetchOpenedMergeRequests`는 `MergeRequestWithApprovals`만 반환하고, 어떤 ProjectID에서 가져왔는지 정보가 없다. 상태 저장의 키로 `ProjectID:MR_IID`를 사용해야 하므로 추적이 필요하다.

**해결: `MergeRequestWithApprovals` 구조체 확장**

```go
// 현재
type MergeRequestWithApprovals struct {
    MergeRequest *gitlab.MergeRequest
    ApprovedBy   []string
}

// 변경
type MergeRequestWithApprovals struct {
    MergeRequest *gitlab.MergeRequest
    ApprovedBy   []string
    ProjectID    int      // 추가 — 어떤 프로젝트에서 가져왔는지
}
```

`gitlab.go:64`의 루프에서 `projectID`를 할당:

```go
allMRs = append(allMRs, &MergeRequestWithApprovals{
    MergeRequest: mr,
    ApprovedBy:   approvedBy,
    ProjectID:    projectID,   // 추가
})
```

### 2.4 테스트 케이스 — `classify_test.go`

| 케이스 | 입력 조건 | 기대 상태 |
|--------|----------|-----------|
| 충돌 있는 MR | HasConflicts=true | `has_conflicts` |
| 변경 요청됨 | ApprovedBy > 0 && < Reviewers | `changes_requested` |
| 블로킹 디스커션 미해결 | BlockingDiscussionsResolved=false, ApprovedBy=0 | `blocking_discussions` |
| 승인됨, 머지 대기 | ApprovedBy ≥ 1, 디스커션 해결 | `approved_pending_merge` |
| 리뷰 대기 | ApprovedBy=0, 디스커션 해결 | `needs_review` |
| 충돌 + 블로킹 (복합) | 둘 다 해당 | `has_conflicts` (우선순위) |
| 리뷰어 없는 MR | Reviewers=[], ApprovedBy=0 | `needs_review`, targets=[reviewer] |

---

## 3. Step 2: 사용자 매핑 + 알림 대상 결정 — `notification.go`, `config.go`

### 3.1 Config 확장

```go
type Config struct {
    // ... 기존 필드

    Notification *NotificationConfig `yaml:"notification"`
    UserMapping  []UserMappingEntry  `yaml:"user_mapping"`
    State        *StateConfig        `yaml:"state"`
}

type NotificationConfig struct {
    Mode    string         `yaml:"mode"`    // "webhook" | "dm"
    Webhook *WebhookConfig `yaml:"webhook"`
    Bot     *BotConfig     `yaml:"bot"`
}

type WebhookConfig struct {
    URL string `yaml:"url"`
}

type BotConfig struct {
    Token string `yaml:"token"`
}

type UserMappingEntry struct {
    GitLabUsername string `yaml:"gitlab_username"`
    SlackID       string `yaml:"slack_id"`
}

type StateConfig struct {
    Path string `yaml:"path"`
}
```

### 3.2 환경변수 지원

| 환경변수 | 대응 설정 |
|---------|----------|
| `NOTIFICATION_MODE` | `notification.mode` |
| `NOTIFICATION_WEBHOOK_URL` | `notification.webhook.url` |
| `NOTIFICATION_BOT_TOKEN` | `notification.bot.token` |
| `STATE_PATH` | `state.path` |

`USER_MAPPING`은 구조가 복잡하므로 YAML 설정 파일 전용.
환경변수로는 지원하지 않는다.

### 3.3 알림 대상 결정 함수

```go
// NotificationTarget은 알림을 보낼 대상이다.
type NotificationTarget struct {
    SlackID        string
    GitLabUsername string
    Role           TargetRole
}

// Notification은 전송할 알림 하나를 나타낸다.
type Notification struct {
    MR      *ClassifiedMR
    Targets []NotificationTarget
    Message string
}

func resolveNotificationTargets(
    classified []*ClassifiedMR,
    mapping []UserMappingEntry,
) []*Notification
```

**동작:**

1. ClassifiedMR의 TargetRoles 확인
2. `RoleAuthor` → MR 작성자의 GitLab username으로 매핑 조회
3. `RoleReviewer` → MR 리뷰어 전원의 GitLab username으로 매핑 조회
4. 매핑 없는 사용자 → `log.Printf("warning: no Slack mapping for GitLab user %q", username)` 후 스킵
5. 상태별 메시지 템플릿 적용

**메시지 템플릿:**

```go
var statusMessages = map[MRStatus]string{
    StatusNeedsReview:          ":eyes: 리뷰를 기다리고 있습니다",
    StatusChangesRequested:     ":pencil2: 변경 요청된 피드백이 있습니다",
    StatusApprovedPendingMerge: ":white_check_mark: 승인됨 — 머지해 주세요",
    StatusHasConflicts:         ":warning: 충돌 해결이 필요합니다",
    StatusBlockingDiscussions:  ":no_entry_sign: 미해결 블로킹 디스커션이 있습니다",
}
```

### 3.4 user_mapping이 비어있을 때 (하위 호환)

`user_mapping`이 비어있거나 없으면:
- `resolveNotificationTargets`는 빈 Targets를 반환
- 전송 단계에서 Targets가 비면 멘션/DM 없이 기존 방식(단순 목록)으로 폴백

### 3.5 테스트 케이스 — `notification_test.go`

| 케이스 | 입력 | 기대 결과 |
|--------|------|-----------|
| 정상 매핑 — 작성자 대상 | author 매핑 있음 | Target에 SlackID 포함 |
| 정상 매핑 — 리뷰어 대상 | reviewer 2명 매핑 있음 | Target에 SlackID 2개 |
| 매핑 누락 | author 매핑 없음 | 경고 로그, 빈 Targets |
| 혼합 (일부 매핑) | reviewer 3명 중 2명만 매핑 | 매핑된 2명만 Target |
| user_mapping 비어있음 | mapping=[] | 전부 빈 Targets |

---

## 4. Step 3: Webhook 멘션 모드 — `slack.go`

### 4.1 Notifier 인터페이스

```go
//go:generate mockery --name Notifier
type Notifier interface {
    Send(notifications []*Notification) error
}
```

### 4.2 WebhookNotifier

```go
type WebhookNotifier struct {
    webhookURL string
}

func (w *WebhookNotifier) Send(notifications []*Notification) error
```

**메시지 포맷:**

한 채널에 모든 알림을 묶어서 전송. 대상별 멘션 삽입:

```
:eyes: *리뷰를 기다리고 있습니다*
<MR_URL|MR Title> (by Author)
→ <@U01234ABCD> <@U05678EFGH>

:white_check_mark: *승인됨 — 머지해 주세요*
<MR_URL|MR Title> (by Author)
→ <@U01234ABCD>
```

**그룹핑**: 같은 상태의 알림을 묶어서 가독성 향상.

```go
func groupNotificationsByStatus(notifications []*Notification) map[MRStatus][]*Notification
```

### 4.3 하위 호환: LegacyNotifier

기존 `notification` 설정이 없을 때 사용하는 레거시 모드:

```go
type LegacyNotifier struct {
    client SlackClient  // 기존 SlackClient 인터페이스
}

func (l *LegacyNotifier) Send(notifications []*Notification) error
```

- 기존 `formatMergeRequestsSummary`와 동일한 포맷으로 전송
- `notification` 설정 없으면 이 경로 사용

### 4.4 Notifier 선택 로직

```go
func newNotifier(config *Config) Notifier {
    if config.Notification == nil {
        // 레거시 모드 — 기존 slack.webhook_url 사용
        return &LegacyNotifier{client: &slackClient{webhookURL: config.Slack.WebhookURL}}
    }

    switch config.Notification.Mode {
    case "dm":
        return &DMNotifier{token: config.Notification.Bot.Token}
    default: // "webhook"
        return &WebhookNotifier{webhookURL: config.Notification.Webhook.URL}
    }
}
```

### 4.5 테스트 — WebhookNotifier

| 케이스 | 입력 | 검증 |
|--------|------|------|
| 멘션 포맷 | Targets=[U123, U456] | 메시지에 `<@U123>` `<@U456>` 포함 |
| 상태별 그룹핑 | needs_review 2건, approved 1건 | 2개 그룹으로 묶임 |
| Targets 비어있음 | 매핑 없는 MR | 멘션 없이 MR 정보만 표시 |

---

## 5. Step 4: Bot DM 모드 — `slack.go`

### 5.1 DMNotifier

```go
type DMNotifier struct {
    token string
}

func (d *DMNotifier) Send(notifications []*Notification) error
```

**동작:**
1. `slack.New(d.token)`으로 Slack API 클라이언트 생성
2. 대상별로 Notification을 그룹핑 (같은 사용자에게 갈 알림을 하나로)
3. 각 대상에게 `client.PostMessage(slackID, ...)` 호출

**대상별 그룹핑:**

```go
func groupNotificationsByTarget(notifications []*Notification) map[string][]*Notification
```

키: SlackID, 값: 해당 사용자에게 보낼 알림 목록

**DM 메시지 포맷:**

```
당신에게 액션이 필요한 MR이 있습니다:

:eyes: *리뷰를 기다리고 있습니다*
• <MR_URL|MR Title> (by Author)

:warning: *충돌 해결이 필요합니다*
• <MR_URL|MR Title> (by Author)
```

### 5.2 SlackAPI 인터페이스 (테스트용)

```go
//go:generate mockery --name SlackAPI
type SlackAPI interface {
    PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
}
```

### 5.3 테스트 — DMNotifier

| 케이스 | 입력 | 검증 |
|--------|------|------|
| 단일 대상 DM | 1명, 알림 1건 | PostMessage 1회 호출 |
| 다중 대상 DM | 3명 | PostMessage 3회 호출 |
| 같은 대상 알림 합침 | 1명에게 3건 | PostMessage 1회, 3건 포함 |
| API 에러 | PostMessage 실패 | 에러 로그 후 계속 (다른 대상은 전송) |

---

## 6. Step 5: 알림 중복 방지 — `state.go`

### 6.1 상태 파일 구조

```json
{
  "notifications": {
    "123:1001": {
      "status": "needs_review",
      "notified_at": "2026-04-12T10:00:00Z"
    },
    "123:1002": {
      "status": "approved_pending_merge",
      "notified_at": "2026-04-12T10:00:00Z"
    }
  },
  "updated_at": "2026-04-12T10:00:00Z"
}
```

- 키: `{ProjectID}:{MR_IID}`
- `status`: 마지막으로 알림 보낸 시점의 MR 상태
- `notified_at`: 알림 전송 시각

### 6.2 타입 정의

```go
type NotificationState struct {
    Notifications map[string]MRNotificationRecord `json:"notifications"`
    UpdatedAt     time.Time                       `json:"updated_at"`
}

type MRNotificationRecord struct {
    Status     MRStatus  `json:"status"`
    NotifiedAt time.Time `json:"notified_at"`
}
```

### 6.3 함수

```go
// 상태 파일 로드. 파일 없으면 빈 상태 반환.
func loadState(path string) (*NotificationState, error)

// 상태 파일 저장.
func saveState(path string, state *NotificationState) error

// 이전 상태와 비교하여 변경된 MR만 필터링.
// - 이전에 없던 MR → 변경됨 (신규)
// - 이전과 상태가 다른 MR → 변경됨
// - 이전과 상태가 같은 MR → 제외
func filterChangedMRs(classified []*ClassifiedMR, prevState *NotificationState) []*ClassifiedMR

// 현재 MR 목록으로 상태 갱신. 닫힌/머지된 MR은 자동 제거.
func buildNewState(classified []*ClassifiedMR) *NotificationState

// 상태 키 생성
func stateKey(projectID, mrIID int) string  // → "123:1001"
```

### 6.4 state.path 미설정 시

`config.State`가 nil이거나 `Path`가 비어있으면:
- `loadState` → 항상 빈 상태 반환 (모든 MR이 "변경됨" 처리)
- `saveState` → no-op
- 결과: 매 실행마다 전체 알림 (현재 동작 유지)

### 6.5 테스트 — `state_test.go`

| 케이스 | 설명 |
|--------|------|
| 파일 없음 → 빈 상태 | 첫 실행 시나리오 |
| 저장 → 로드 라운드트립 | JSON 직렬화/역직렬화 |
| 신규 MR 감지 | 이전 상태에 없던 MR → 변경됨 |
| 상태 변경 감지 | needs_review → approved → 변경됨 |
| 상태 동일 → 필터됨 | needs_review → needs_review → 제외 |
| stale MR 제거 | 이전에 있던 MR이 현재 목록에 없음 → 새 상태에서 삭제 |

---

## 7. Step 6: execute() 통합 — `main.go`

### 7.1 리팩토링된 execute()

```go
func execute(config *Config) error {
    glClient, err := gitlab.NewClient(config.GitLab.Token,
        gitlab.WithBaseURL(config.GitLab.URL))
    if err != nil {
        return fmt.Errorf("error creating GitLab client: %w", err)
    }

    gitlabClient := &gitLabClient{client: glClient}

    // 1. MR 조회
    mrs, err := fetchOpenedMergeRequests(config, gitlabClient)
    if err != nil {
        return fmt.Errorf("error fetching opened merge requests: %w", err)
    }

    // 레거시 모드: notification 설정 없으면 기존 흐름
    if config.Notification == nil {
        return executeLegacy(config, mrs)
    }

    // 2. 상태 분류
    classified := classifyMergeRequests(mrs)

    // 3. 중복 방지 — 상태 변경된 MR만 추출
    statePath := ""
    if config.State != nil {
        statePath = config.State.Path
    }
    prevState, err := loadState(statePath)
    if err != nil {
        log.Printf("Warning: could not load state: %v", err)
        prevState = &NotificationState{Notifications: make(map[string]MRNotificationRecord)}
    }

    changed := filterChangedMRs(classified, prevState)

    if len(changed) == 0 {
        log.Println("No MR status changes detected.")
        return saveState(statePath, buildNewState(classified))
    }

    // 4. 알림 대상 결정
    notifications := resolveNotificationTargets(changed, config.UserMapping)

    // 5. 전송
    notifier := newNotifier(config)
    if err := notifier.Send(notifications); err != nil {
        return fmt.Errorf("error sending notifications: %w", err)
    }

    // 6. 상태 저장
    if err := saveState(statePath, buildNewState(classified)); err != nil {
        log.Printf("Warning: could not save state: %v", err)
    }

    log.Printf("Sent %d notifications for %d MRs.", len(notifications), len(changed))
    return nil
}

// 레거시 모드 — 기존 동작 그대로
func executeLegacy(config *Config, mrs []*MergeRequestWithApprovals) error {
    mrs = filterMergeRequestsByAuthor(mrs, config.Authors)
    if len(mrs) == 0 {
        log.Println("No opened merge requests found.")
        return nil
    }
    summary := formatMergeRequestsSummary(mrs)
    slackClient := &slackClient{webhookURL: config.Slack.WebhookURL}
    return sendSlackMessage(slackClient, summary)
}
```

### 7.2 모드 분기 요약

```
config.Notification == nil?
  ├─ yes → executeLegacy() [기존 동작 100% 유지]
  └─ no  → 스마트 알림 흐름
              ├─ mode: "webhook" → WebhookNotifier (채널 멘션)
              └─ mode: "dm"      → DMNotifier (개인 DM)
```

### 7.3 테스트 — `main_test.go` (추가)

| 케이스 | 설명 |
|--------|------|
| 레거시 모드 | notification=nil → 기존 흐름 동작 확인 |
| Webhook 모드 + 상태 변경 | 변경된 MR만 알림 전송 |
| DM 모드 | DMNotifier.Send 호출 확인 |
| 상태 변경 없음 | "No MR status changes" 로그, 전송 없음 |
| 상태 파일 경로 없음 | 매 실행마다 전체 알림 |

---

## 8. 구현 순서 (최종)

```
Phase 1 (병렬):
  ┌─ Step 1: classify.go + classify_test.go
  └─ Step 2: config.go 확장 + notification.go + 테스트

Phase 2 (Phase 1 완료 후, 병렬):
  ┌─ Step 3: WebhookNotifier (slack.go 확장)
  ├─ Step 4: DMNotifier (slack.go 확장)
  └─ Step 5: state.go + state_test.go

Phase 3 (Phase 2 완료 후):
  └─ Step 6: main.go 리팩토링 + 통합 테스트

Phase 4 (마무리):
  └─ config.yaml.example 갱신, mock 재생성, README 업데이트
```

---

## 9. 대안 검토

### 9.1 단일 상태 vs 다중 상태

| 방안 | 장점 | 단점 |
|------|------|------|
| **A: 단일 상태 (채택)** | 알림 명확, 우선순위로 가장 중요한 것만 전달 | 부수 상태 누락 가능 |
| B: 다중 상태 | 모든 상태 알림 | 한 MR에 여러 알림 → 노이즈 |

**결정**: 우선순위 기반 단일 상태. 가장 긴급한 것을 해결하면 다음 상태가 자연스럽게 노출된다.

### 9.2 Notifier 인터페이스 vs 직접 분기

| 방안 | 장점 | 단점 |
|------|------|------|
| **A: 인터페이스 (채택)** | 테스트 용이, 모드 추가 쉬움 | 코드량 증가 |
| B: switch 분기 | 단순 | 테스트 어려움, 확장 시 비대 |

**결정**: Notifier 인터페이스. mock으로 테스트 가능하고, 향후 모드 추가 (예: 이메일) 시 구현체만 추가.

### 9.3 상태 저장: 단일 파일 vs 인터페이스

| 방안 | 장점 | 단점 |
|------|------|------|
| **A: 단일 파일 (채택)** | 가장 간단, Docker Volume으로 충분 | Lambda 미지원 |
| B: StateStore 인터페이스 | Lambda/DynamoDB 확장 가능 | YAGNI, 현재 불필요 |

**결정**: JSON 파일 직접 사용. Docker standalone 확정이므로 충분. 필요 시 향후 인터페이스화.
