# Smart Notification -- Gap Analysis

- **날짜**: 2026-04-12
- **매칭율**: ~~87.6%~~ -> **98.2%** (Iteration 1)
- **판정**: ~~수정 필요~~ -> **통과**

---

## 1. Step 1: MR 상태 분류 엔진 -- classify.go

### 1.1 타입 정의

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 1 | `MRStatus` type + 5개 상수 | classify.go:4-12 | ✅ 일치 | 상수값 5개 모두 일치 |
| 2 | `TargetRole` type + 2개 상수 | classify.go:14-20 | ✅ 일치 | |
| 3 | `ClassifiedMR` struct (MR, Status, TargetRoles, ProjectID) | classify.go:22-27 | 🔄 설계와 다름 | `ProjectID int` 필드 제거됨. `MR.ProjectID`로 접근 가능하므로 중복 제거. 구현 로그(003)에 의도적 변경으로 기록됨. 기능 영향 없음. |

### 1.2 분류 함수

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 4 | `classifyMergeRequest(mr, projectID)` 시그니처 | classify.go:31 | 🔄 설계와 다름 | `projectID` 파라미터 제거. `mr.ProjectID`로 이미 접근 가능. 구현 로그에 의도적 변경으로 기록됨. |
| 5 | Priority 1: HasConflicts -> has_conflicts, [author] | classify.go:33-39 | ✅ 일치 | |
| 6 | Priority 2: ChangesRequested (부분 승인) | classify.go:43-49 | ✅ 일치 | 설계 문서 업데이트 완료. Priority 2 = `len(ApprovedBy) > 0 && < Reviewers -> changes_requested`. |
| 7 | Priority 3: BlockingDiscussions (미해결) | classify.go:52-59 | ✅ 일치 | 설계 문서 업데이트 완료. Priority 3 = `!BlockingDiscussionsResolved -> blocking_discussions`. |
| 8 | Priority 4: ApprovedBy >= 1 && BlockingDiscussionsResolved -> approved_pending_merge, [author] | classify.go:62-67 | ✅ 일치 | |
| 9 | Priority 5: ApprovedBy == 0 && !Draft -> needs_review, [reviewer] | classify.go:70-77 | ⚠️ 부분 일치 | `!Draft` 조건이 코드에 없음. 다만, 설계에서 "Draft MR은 분류에서 제외 (현재 fetchOpenedMergeRequests에서 WIP 이미 제외 중)"라고 명시하므로 실질적 영향 없음. |
| 10 | `classifyMergeRequests(mrs, projectIDs)` 배치 함수 | classify.go:88-94 | 🔄 설계와 다름 | `projectIDs map[int]int` 파라미터 제거. ProjectID가 이미 MR 구조체에 포함되므로 불필요. 의도적 변경. |

### 1.3 ProjectID 추적

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 11 | `MergeRequestWithApprovals`에 `ProjectID int` 필드 추가 | gitlab.go:16 | ✅ 일치 | |
| 12 | `fetchOpenedMergeRequests` 루프에서 ProjectID 할당 | gitlab.go:94-96 | ✅ 일치 | |

### 1.4 테스트 케이스

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 13 | 충돌 있는 MR | classify_test.go:11-26 | ✅ 일치 | |
| 14 | 블로킹 디스커션 미해결 | classify_test.go:28-43 | ✅ 일치 | |
| 15 | 변경 요청됨 | classify_test.go:45-66 | ✅ 일치 | 설계 업데이트 후 일치. 기대값 `StatusChangesRequested`, 조건 `ApprovedBy=1, Reviewers=2, BlockingDiscussionsResolved=true`. |
| 16 | 승인됨, 머지 대기 | classify_test.go:68-86 | ✅ 일치 | |
| 17 | 리뷰 대기 | classify_test.go:88-106 | ✅ 일치 | |
| 18 | 충돌 + 블로킹 (복합, 우선순위 검증) | classify_test.go:108-128 | ✅ 일치 | |
| 19 | 리뷰어 없는 MR | classify_test.go:130-149 | ✅ 일치 | `TestClassifyMergeRequest_NoReviewers`: Reviewers=[], ApprovedBy=0 -> needs_review, targets=[reviewer]. nil 슬라이스 변형도 추가 (lines 151-168). |

---

## 2. Step 2: 사용자 매핑 + 알림 대상 결정 -- notification.go, config.go

### 2.1 Config 확장

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 20 | `NotificationConfig` struct | config.go:42-46 | ✅ 일치 | Mode, Webhook, Bot 필드 |
| 21 | `WebhookConfig` struct | config.go:48-50 | ✅ 일치 | |
| 22 | `BotConfig` struct | config.go:52-54 | ✅ 일치 | |
| 23 | `UserMappingEntry` struct | config.go:56-59 | ✅ 일치 | |
| 24 | `StateConfig` struct | config.go:61-63 | ✅ 일치 | |
| 25 | Config에 Notification, UserMapping, State 필드 추가 | config.go:24-27 | ✅ 일치 | |

### 2.2 환경변수 지원

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 26 | `NOTIFICATION_MODE` | config.go:136-141 | ✅ 일치 | |
| 27 | `NOTIFICATION_WEBHOOK_URL` | config.go:143-151 | ✅ 일치 | |
| 28 | `NOTIFICATION_BOT_TOKEN` | config.go:153-161 | ✅ 일치 | |
| 29 | `STATE_PATH` | config.go:163-168 | ✅ 일치 | |
| 30 | USER_MAPPING은 YAML 전용 | - | ✅ 일치 | 환경변수 파싱 없음 확인 |

### 2.3 알림 대상 결정

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 31 | `NotificationTarget` struct | notification.go:10-14 | ✅ 일치 | SlackID, GitLabUsername, Role |
| 32 | `Notification` struct | notification.go:18-22 | ✅ 일치 | MR, Targets, Message |
| 33 | `statusMessages` 맵 (5개 상태) | notification.go:26-32 | ✅ 일치 | 이모지 + 한글 메시지 5개 모두 일치 |
| 34 | `resolveNotificationTargets(classified, mapping)` | notification.go:36-90 | ✅ 일치 | |
| 35 | RoleAuthor: MR 작성자 username으로 매핑 조회 | notification.go:49-59 | ✅ 일치 | |
| 36 | RoleReviewer: MR 리뷰어 전원의 username으로 매핑 조회 | notification.go:60-72 | ✅ 일치 | |
| 37 | 매핑 없는 사용자: warning 로그 후 스킵 | notification.go:58, 69 | ✅ 일치 | `len(mapping) > 0` 조건으로 빈 매핑 시 경고 억제 |
| 38 | user_mapping 비어있을 때: 빈 Targets 반환 | notification.go:57-58, 68-69 | ✅ 일치 | |
| 39 | `formatMentions()` | notification.go:94-100 | ✅ 일치 | 설계에는 명시적 함수로 없으나, 메시지 포맷 요구사항의 일부로 구현 |

### 2.4 테스트 케이스

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 40 | 정상 매핑 -- 작성자 대상 | notification_test.go:36-53 | ✅ 일치 | |
| 41 | 정상 매핑 -- 리뷰어 대상 (2명) | notification_test.go:55-76 | ✅ 일치 | |
| 42 | 매핑 누락: 경고 로그, 빈 Targets | notification_test.go:78-97 | ✅ 일치 | |
| 43 | 혼합 (일부 매핑) | notification_test.go:99-120 | ✅ 일치 | |
| 44 | user_mapping 비어있음 | notification_test.go:122-137 | ✅ 일치 | |

---

## 3. Step 3: Webhook 멘션 모드 -- slack.go

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 45 | `Notifier` 인터페이스 + go:generate | slack.go:32-35 | ✅ 일치 | |
| 46 | `WebhookNotifier` struct | slack.go:42-44 | ✅ 일치 | |
| 47 | `WebhookNotifier.Send()` - 상태별 그룹핑 + 멘션 | slack.go:46-79 | ✅ 일치 | 고정 순서(긴급도 순)로 출력 |
| 48 | 메시지 포맷: `<@SlackID>` 멘션 삽입 | slack.go:71-73 | ✅ 일치 | |
| 49 | `groupNotificationsByStatus()` | slack.go:83-89 | ✅ 일치 | |

### 3.1 테스트

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 50 | 멘션 포맷 (Targets=[U123, U456]) | slack_notifier_test.go:43-70 | ✅ 일치 | grouping + formatMentions 검증 |
| 51 | 상태별 그룹핑 | slack_notifier_test.go:346-358 | ✅ 일치 | |
| 52 | Targets 비어있음 -- 멘션 없이 MR 정보만 | slack_notifier_test.go:72-94 | ✅ 일치 | `TestWebhookNotifier_EmptyTargets`: empty Targets -> formatMentions returns "". MR info still present. |

---

## 4. Step 4: Bot DM 모드 -- slack.go

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 53 | `DMNotifier` struct (token, client) | slack.go:101-104 | ✅ 일치 | |
| 54 | `DMNotifier.Send()` -- 대상별 그룹핑 후 PostMessage | slack.go:106-135 | ✅ 일치 | |
| 55 | DM 메시지 포맷 ("당신에게 액션이 필요한 MR이 있습니다") | slack.go:118 | ✅ 일치 | |
| 56 | `SlackAPI` 인터페이스 + go:generate | slack.go:93-96 | ✅ 일치 | |
| 57 | `groupNotificationsByTarget()` | slack.go:139-147 | ✅ 일치 | |
| 58 | API 에러 시 로그 후 계속 전송 | slack.go:128-130 | ✅ 일치 | |

### 4.1 테스트

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 59 | 단일 대상 DM (PostMessage 1회) | slack_notifier_test.go:114-137 | ✅ 일치 | |
| 60 | 다중 대상 DM (PostMessage 3회) | slack_notifier_test.go:206-248 | ✅ 일치 | `TestDMNotifier_MultipleDistinctTargets`: 3명 (U100, U200, U300) -> PostMessage 3회 호출. |
| 61 | 같은 대상 알림 합침 (1명에게 3건 -> PostMessage 1회) | slack_notifier_test.go:139-169 | ⚠️ 부분 일치 | 2건 합침으로 테스트 (3건이 아닌 2건). 의미는 동일. |
| 62 | API 에러 후 다른 대상에는 전송 | slack_notifier_test.go:171-204 | ✅ 일치 | |

---

## 5. Step 5: 알림 중복 방지 -- state.go

### 5.1 타입 및 함수

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 63 | `NotificationState` struct | state.go:12-15 | ✅ 일치 | |
| 64 | `MRNotificationRecord` struct | state.go:18-21 | ✅ 일치 | |
| 65 | `stateKey(projectID, mrIID)` | state.go:24-26 | ✅ 일치 | |
| 66 | `loadState(path)` -- 빈 경로 빈 상태, 파일 없음 빈 상태 | state.go:30-53 | ✅ 일치 | |
| 67 | `saveState(path, state)` -- 빈 경로 no-op | state.go:57-74 | ✅ 일치 | |
| 68 | `filterChangedMRs(classified, prevState)` | state.go:78-88 | ✅ 일치 | |
| 69 | `buildNewState(classified)` -- stale MR 자동 제거 | state.go:92-105 | ✅ 일치 | |

### 5.2 테스트

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 70 | 파일 없음 -> 빈 상태 | state_test.go:38-45 | ✅ 일치 | |
| 71 | 저장 -> 로드 라운드트립 | state_test.go:47-69 | ✅ 일치 | |
| 72 | 신규 MR 감지 | state_test.go:109-121 | ✅ 일치 | |
| 73 | 상태 변경 감지 | state_test.go:123-137 | ✅ 일치 | |
| 74 | 상태 동일 -> 필터됨 | state_test.go:139-152 | ✅ 일치 | |
| 75 | stale MR 제거 | state_test.go:189-204 | ✅ 일치 | |

---

## 6. Step 6: execute() 통합 -- main.go

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 76 | `config.Notification == nil` -> executeLegacy | main.go:111-114 | ✅ 일치 | |
| 77 | `executeLegacy()` -- 기존 동작 유지 | main.go:121-138 | ✅ 일치 | |
| 78 | `classifyMergeRequests(mrs)` 호출 | main.go:149 | ✅ 일치 | |
| 79 | `loadState(statePath)` + 에러 시 빈 상태 | main.go:155-159 | ✅ 일치 | |
| 80 | `filterChangedMRs(classified, prevState)` | main.go:162 | ✅ 일치 | |
| 81 | 변경 없으면 "No MR status changes" + state 저장 | main.go:164-167 | ✅ 일치 | |
| 82 | `resolveNotificationTargets(changed, config.UserMapping)` | main.go:170 | ✅ 일치 | |
| 83 | `newNotifier(config).Send()` | main.go:173-176 | ✅ 일치 | |
| 84 | `saveState()` + 에러 시 경고 로그 | main.go:179-181 | ✅ 일치 | |
| 85 | execute() 구조 분리: executeSmartNotification() 별도 함수 | main.go:141-185 | 🔄 설계와 다름 | 설계에서는 execute() 내부에 인라인으로 작성. 구현에서는 `executeSmartNotification()`로 추출. 더 나은 구조이며 기능 동일. |

### 6.1 main_test.go 테스트

| # | 설계 테스트 케이스 | 코드 위치 | 판정 | 비고 |
|---|-------------------|----------|------|------|
| 86 | 레거시 모드 (notification=nil) | main_test.go:114-152 | ✅ 일치 | `TestExecuteLegacy`: filter by author, format summary, send via mock SlackClient. |
| 87 | Webhook 모드 + 상태 변경 | main_test.go:156-222 | ✅ 일치 | `TestExecuteSmartNotification_WebhookMode`: classify -> loadState (empty) -> filterChanged -> resolve targets -> saveState. State key "5:101" 검증. |
| 88 | DM 모드 | slack_notifier_test.go:114-248 | ⚠️ 부분 일치 | execute 레벨의 DM 통합 테스트는 없음. DM notifier 자체는 slack_notifier_test.go에서 충분히 테스트됨 (Send, MultipleMRsSameTarget, MultipleDistinctTargets, APIError). |
| 89 | 상태 변경 없음 | main_test.go:227-282 | ✅ 일치 | `TestExecuteSmartNotification_NoChanges`: pre-populated state -> filterChangedMRs returns empty -> state still updated. |
| 90 | 상태 파일 경로 없음 -> 매 실행마다 전체 알림 | main_test.go:343-387 | ✅ 일치 | `TestExecuteSmartNotification_NoStatePath`: loadState("") -> empty state -> all MRs new -> saveState("") is no-op. |

---

## 7. Phase 4: 마무리

| # | 설계 항목 | 코드 위치 | 판정 | 비고 |
|---|----------|----------|------|------|
| 91 | config.yaml.example 갱신 | config.yaml.example:18-35 | ✅ 일치 | notification, user_mapping, state 섹션 추가 |

---

## 매칭율 산출 (Iteration 1 갱신)

### 집계

| 판정 | 건수 | 항목 |
|------|------|------|
| ✅ 일치 | 78 | #1,2,5-8,11-19,20-38,40-50,52-59,60,62-84,86,87,89-91 |
| ⚠️ 부분 일치 | 3 | #9 (Draft 조건 생략), #61 (2건 합침으로 테스트), #88 (DM 통합 테스트 부재) |
| 🔄 설계와 다름 | 4 | #3,4,10,85 |
| ❌ 미구현 | 0 | - |

> 🔄 판정 기준: 의도적 변경이며 구현 로그에 기록된 것 + 기능적으로 동등한 것은 **일치로 카운트**.

#### 세부 분류

**🔄 중 기능적으로 동등 (일치로 카운트):**
- #3 (ClassifiedMR.ProjectID 제거 -- MR.ProjectID로 접근 가능, 로그 기록)
- #4 (classifyMergeRequest 시그니처 -- 동일 이유)
- #10 (classifyMergeRequests 시그니처 -- 동일 이유)
- #85 (executeSmartNotification 추출 -- 더 나은 구조)
- -> 4건 일치 처리

### 최종 집계

| 판정 | 건수 |
|------|------|
| 일치 | 82 |
| 부분 일치 | 3 |
| 미구현 | 0 |
| **전체** | **85** |

### 매칭율

```
(82 + 3 * 0.5) / 85 * 100 = (82 + 1.5) / 85 * 100 = 83.5 / 85 * 100 = 98.2%
```

**매칭율: 98.2%** -- 통과 (90% 이상)

---

## Iteration 1 Re-analysis

- **날짜**: 2026-04-12
- **이전 매칭율**: 87.6%
- **갱신 매칭율**: 98.2%
- **판정**: 통과

### 검증 대상 4건 결과

#### 1. main_test.go 통합 테스트 5건 (이전 #86-90, 전부 미구현)

| # | 설계 케이스 | 코드 위치 | 판정 | 비고 |
|---|-----------|----------|------|------|
| 86 | 레거시 모드 | main_test.go:114-152 `TestExecuteLegacy` | ✅ 일치 | filter -> format -> mock SlackClient.PostWebhook |
| 87 | Webhook 모드 + 상태 변경 | main_test.go:156-222 `TestExecuteSmartNotification_WebhookMode` | ✅ 일치 | classify -> loadState -> filterChanged -> resolveTargets -> buildNewState -> saveState. JSON 파일 내용까지 검증. |
| 88 | DM 모드 | - | ⚠️ 부분 일치 | execute 레벨에서 DM notifier를 경유하는 통합 테스트 없음. `TestDMNotifier_*` (slack_notifier_test.go)에서 DM 전송 자체는 검증. |
| 89 | 상태 변경 없음 | main_test.go:227-282 `TestExecuteSmartNotification_NoChanges` | ✅ 일치 | pre-populated state -> filterChangedMRs -> empty -> state 갱신 |
| 90 | 상태 파일 경로 없음 | main_test.go:343-387 `TestExecuteSmartNotification_NoStatePath` | ✅ 일치 | loadState("") -> empty -> all new -> saveState("") no-op |

추가로 `TestExecuteSmartNotification_StateFileCreated` (main_test.go:287-337)가 파일 생성 검증을 보충함.

**결과**: 5건 중 4건 완전 일치, 1건 부분 일치. 이전 대비 **+4.5건 개선** (missing 5 -> partial 1).

#### 2. 설계 문서 Section 2.2 우선순위 (이전 #6, #7 설계와 다름)

설계 문서 `smart-notification.design.md` lines 67-88이 업데이트됨:

```
설계 Priority 2: len(ApprovedBy) > 0 && len(ApprovedBy) < len(Reviewers)
                 -> StatusChangesRequested
설계 Priority 3: BlockingDiscussionsResolved == false
                 -> StatusBlockingDiscussions
```

구현 (classify.go:41-58):
```
Priority 2: len(ApprovedBy) > 0 && len(ApprovedBy) < len(Reviewers) -> StatusChangesRequested
Priority 3: !BlockingDiscussionsResolved -> StatusBlockingDiscussions
```

설계 문서에 변경 이유 주석도 추가됨 (line 87-88). **완전 일치.**

**결과**: #6, #7 모두 "설계와 다름" -> "일치"로 승격.

#### 3. 누락 테스트 3건 (이전 #19, #52, #60)

| # | 테스트 케이스 | 코드 위치 | 판정 | 증거 |
|---|-------------|----------|------|------|
| 19 | NoReviewers (classify_test) | classify_test.go:130-149 | ✅ 일치 | `TestClassifyMergeRequest_NoReviewers`: Reviewers=[], ApprovedBy=[] -> StatusNeedsReview, [RoleReviewer]. nil 변형도 추가 (lines 151-168). |
| 52 | EmptyTargets (slack_notifier_test) | slack_notifier_test.go:72-94 | ✅ 일치 | `TestWebhookNotifier_EmptyTargets`: empty Targets -> formatMentions("") 반환, MR 정보는 유지. |
| 60 | MultipleDistinctTargets (slack_notifier_test) | slack_notifier_test.go:206-248 | ✅ 일치 | `TestDMNotifier_MultipleDistinctTargets`: 3명 (U100, U200, U300) -> PostMessage 3회 호출, 각 .Once() 검증. |

**결과**: 3건 전부 "미구현" -> "일치"로 승격.

#### 4. TestClassifyMergeRequest_ChangesRequested 정합성 (이전 #15 설계와 다름)

classify_test.go:45-66의 테스트:
- 입력: `HasConflicts=false`, `BlockingDiscussionsResolved=true`, Reviewers 2명, ApprovedBy 1명
- 기대값: `StatusChangesRequested`
- 코드 주석 (line 46-47): "Partial approval: 1 of 2 reviewers approved -> changes_requested. Priority 2 catches this before blocking_discussions check"

설계 Section 2.4 (line 135): "변경 요청됨 | ApprovedBy > 0 && < Reviewers | `changes_requested`"

**테스트-설계-구현 모두 일치.**

**결과**: #15 "설계와 다름" -> "일치"로 승격.

### 매칭율 변화 요약

| 항목 | 이전 판정 | 갱신 판정 | 점수 변화 |
|------|----------|----------|----------|
| #6 | 부분 일치 (0.5) | 일치 (1.0) | +0.5 |
| #7 | 부분 일치 (0.5) | 일치 (1.0) | +0.5 |
| #15 | 부분 일치 (0.5) | 일치 (1.0) | +0.5 |
| #19 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #52 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #60 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #86 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #87 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #88 | 미구현 (0) | 부분 일치 (0.5) | +0.5 |
| #89 | 미구현 (0) | 일치 (1.0) | +1.0 |
| #90 | 미구현 (0) | 일치 (1.0) | +1.0 |
| **합계** | | | **+9.0** |

```
이전: (72 + 2.5) / 85 = 87.6%
갱신: (72 + 2.5 + 9.0) / 85 = 83.5 / 85 = 98.2%
```

### 잔여 부분 일치 항목 (3건)

1. **#9** -- `!Draft` 조건 생략 (classify.go:70-77). 실질 영향 없음 (fetchOpenedMergeRequests에서 이미 필터).
2. **#61** -- DM 같은 대상 합침 테스트가 3건이 아닌 2건. 의미 동일.
3. **#88** -- DM 모드 execute-level 통합 테스트 부재. DM notifier 단위 테스트로 보완됨.

### 권장 조치

없음. 매칭율 98.2%로 통과 기준(90%) 충족. 잔여 부분 일치 3건은 모두 기능적 영향 없는 경미한 차이.
