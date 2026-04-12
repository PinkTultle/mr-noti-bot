# 완료 보고서: MR 상태 기반 스마트 알림 (smart-notification)

- **작성일**: 2026-04-12
- **기능**: smart-notification
- **PDCA 사이클**: 1 Iteration (Plan → Design → Do → Check → Act)
- **최종 매칭율**: 98.2% (Gap Analysis)

---

## 1. Executive Summary

### Value Delivered

| 관점 | 내용 |
|------|------|
| **Problem** | 열린 MR 목록을 단일 채널에 일괄 전송하여, 누가 어떤 액션을 해야 하는지 구분이 없었다 |
| **Solution** | MR 상태를 5가지로 분류하고, 상태별 행동 주체(작성자/리뷰어)를 결정하여 Slack 멘션 또는 DM으로 직접 알린다 |
| **Function UX Effect** | 각 담당자가 자신이 해야 할 일(리뷰, 피드백 반영, 머지, 충돌 해결)을 Slack에서 즉시 확인한다 |
| **Core Value** | MR 처리 속도 향상 — 리뷰 대기 시간 단축, 머지 지연 방지 |

### 변경 규모 요약

| 구분 | 수치 | 출처 |
|------|------|------|
| 신규 파일 (.go) | 6개 (classify.go, notification.go, state.go + 3개 _test.go) | 구현 |
| 수정 파일 | 5개 (config.go, gitlab.go, slack.go, main.go, config.yaml.example) | 구현 |
| 테스트 함수 합계 | 60개 | grep |
| 전체 소스 라인 | 2,635줄 (13개 파일 합계) | wc -l |
| 설계-구현 매칭율 | 87.6% → 98.2% (Iteration 1회) | Gap Analysis |

---

## 2. PDCA 사이클 타임라인

| 단계 | 산출물 | 주요 결정 사항 |
|------|--------|---------------|
| **Plan** | smart-notification.plan.md | 5가지 MR 상태 분류, Webhook/DM 이중 모드, JSON 상태 파일 중복 방지, 레거시 모드 하위 호환 |
| **Design** | smart-notification.design.md | 6단계 Step 구조, Notifier 인터페이스 패턴, 우선순위 재조정 (changes_requested > blocking_discussions) |
| **Do** | classify.go, notification.go, state.go, slack.go, config.go, main.go 외 | 설계 대비 ClassifiedMR.ProjectID 필드 중복 제거, executeSmartNotification() 함수 분리 |
| **Check** | smart-notification.analysis.md | 초기 87.6% → 재분석 후 98.2%; 5건 통합 테스트 추가, 3건 단위 테스트 추가 |
| **Act** | (설계 문서 인라인 업데이트) | 우선순위 순서 설계 문서 반영, 잔여 부분 일치 3건은 기능 영향 없음으로 확정 |

---

## 3. 구현 요약

### 3.1 신규 파일

| 파일 | 역할 | 라인 수 | 테스트 함수 수 |
|------|------|---------|--------------|
| `classify.go` | MR → 5가지 상태 분류 엔진 (우선순위 기반) | 94 | - |
| `classify_test.go` | 상태 분류 테스트 (9케이스) | 216 | 9 |
| `notification.go` | 알림 대상 결정, 메시지 템플릿, 멘션 포맷 | 100 | - |
| `notification_test.go` | 알림 대상 결정 테스트 (7케이스) | 152 | 7 |
| `state.go` | JSON 상태 파일 로드/저장/비교/stale 제거 | 105 | - |
| `state_test.go` | 상태 파일 테스트 (14케이스) | 212 | 14 |

### 3.2 수정 파일

| 파일 | 주요 변경 내용 | 라인 수 |
|------|--------------|---------|
| `slack.go` | Notifier 인터페이스, WebhookNotifier, DMNotifier, LegacyNotifier 추가 | 182 |
| `slack_notifier_test.go` | 3가지 Notifier 테스트 (19케이스) | 408 |
| `config.go` | NotificationConfig, UserMappingEntry, StateConfig 구조체 + 환경변수 파싱 | 249 |
| `config_test.go` | 새 설정 로드 테스트 (1케이스) | 121 |
| `main.go` | execute() → executeLegacy() + executeSmartNotification() 분리 | 233 |
| `main_test.go` | 통합 테스트 (10케이스) | 392 |
| `gitlab.go` | MergeRequestWithApprovals에 ProjectID 필드 추가 | 171 |

### 3.3 테스트 현황

| 파일 | 테스트 함수 수 | 검증 범위 |
|------|--------------|----------|
| classify_test.go | 9 | 5가지 상태 분류 + 복합 조건 + 리뷰어 없는 케이스 |
| notification_test.go | 7 | 매핑 정상/누락/혼합/비어있음 케이스 |
| state_test.go | 14 | 파일 없음, 라운드트립, 신규/변경/동일/stale 제거 |
| slack_notifier_test.go | 19 | WebhookNotifier(멘션/그룹핑), DMNotifier(단일/다중/합침/에러) |
| config_test.go | 1 | 새 설정 필드 로드 |
| main_test.go | 10 | 레거시, Webhook, 상태변경 없음, 상태파일 없음, 파일 생성 |
| **합계** | **60** | |

---

## 4. Gap Analysis 결과

출처: `docs/03-analysis/smart-notification.analysis.md`

### 4.1 매칭율 변화

| 시점 | 매칭율 | 판정 |
|------|--------|------|
| 초기 (구현 직후) | 87.6% | 수정 필요 |
| Iteration 1 (재분석) | 98.2% | 통과 |

**산출 공식** (Iteration 1): `(82 + 3 × 0.5) / 85 × 100 = 98.2%`

### 4.2 최종 판정 집계

| 판정 | 건수 |
|------|------|
| 일치 (✅) | 82 |
| 부분 일치 (⚠️) | 3 |
| 미구현 (❌) | 0 |
| **전체** | **85** |

### 4.3 초기 → Iteration 1 개선 내역

| 항목 | 이전 | 갱신 | 점수 변화 |
|------|------|------|----------|
| #6 우선순위 순서 (changes_requested) | 부분 일치 | 일치 | +0.5 |
| #7 우선순위 순서 (blocking_discussions) | 부분 일치 | 일치 | +0.5 |
| #15 TestChangesRequested 정합성 | 부분 일치 | 일치 | +0.5 |
| #19 TestNoReviewers 추가 | 미구현 | 일치 | +1.0 |
| #52 TestEmptyTargets 추가 | 미구현 | 일치 | +1.0 |
| #60 TestMultipleDistinctTargets 추가 | 미구현 | 일치 | +1.0 |
| #86~87, #89~90 통합 테스트 4건 추가 | 미구현 | 일치 | +4.0 |
| #88 DM 통합 테스트 | 미구현 | 부분 일치 | +0.5 |

---

## 5. 설계 대비 의도적 변경 사항

| # | 변경 내용 | 설계 | 구현 | 이유 |
|---|---------|------|------|------|
| 1 | ClassifiedMR.ProjectID 필드 제거 | `ClassifiedMR`에 `ProjectID int` 포함 | 필드 없음 | `MR.ProjectID`로 이미 접근 가능하여 중복 제거 |
| 2 | classifyMergeRequest 시그니처 변경 | `(mr, projectID int)` | `(mr)` | 위와 동일 이유 |
| 3 | classifyMergeRequests 시그니처 변경 | `(mrs, projectIDs map[int]int)` | `(mrs)` | 위와 동일 이유 |
| 4 | executeSmartNotification() 분리 | execute() 내부 인라인 | 별도 함수로 추출 | 가독성 및 단위 테스트 용이성 향상 |
| 5 | 우선순위 순서 조정 | blocking > changes_requested | changes_requested > blocking | 기존 순서에서 changes_requested가 도달 불가능한 논리 결함 수정 |

---

## 6. 잔여 부분 일치 항목 (기능 영향 없음)

| # | 항목 | 차이 내용 | 기능 영향 |
|---|------|---------|---------|
| #9 | `!Draft` 조건 생략 | classify.go의 needs_review 분기에 Draft 체크 없음 | 없음 — fetchOpenedMergeRequests에서 WIP/Draft 이미 제외 |
| #61 | DM 합침 테스트 규모 | 설계: 3건 합침, 구현: 2건 합침으로 테스트 | 없음 — 합침 동작 자체는 동일하게 검증 |
| #88 | DM 모드 execute-level 통합 테스트 | execute()에서 DM notifier를 경유하는 E2E 테스트 부재 | 없음 — DMNotifier.Send() 자체는 slack_notifier_test.go에서 충분히 검증 |

---

## 7. 알려진 한계 및 향후 작업

### 7.1 Out-of-Scope 항목 (Plan에서 명시)

| 항목 | 분리 이유 |
|------|---------|
| scheduled-summary (정기 MR 현황 요약) | 별도 기능으로 분리 — 현재 smart-notification과 목적이 다름 |
| Slack 인터랙티브 버튼 (머지/승인 액션) | 별도 아키텍처 필요 (Slack Actions 엔드포인트) |
| GitLab Webhook 이벤트 기반 실시간 알림 | 현재 폴링 방식 — 웹서버 추가 필요 |
| Lambda/K8s CronJob용 원격 상태 저장소 | DynamoDB 등 외부 저장소 의존 — Docker standalone 확정 |

### 7.2 기술 부채

| 항목 | 내용 | 향후 조치 |
|------|------|---------|
| StateStore 인터페이스화 | 현재 JSON 파일 직접 사용 (Docker Volume 전제) | Lambda/K8s 지원 필요 시 StateStore 인터페이스 도입 후 FileStateStore/DynamoDBStateStore 구현 |
| DM 모드 execute-level 통합 테스트 (#88) | execute() 레벨에서 DMNotifier 경로 E2E 테스트 없음 | main_test.go에 TestExecuteSmartNotification_DMMode 추가 |
| user_mapping 없을 때 Webhook 폴백 | user_mapping이 비어있으면 멘션 없이 MR 정보만 전송 (명시적 경고 없음) | 설정 누락 시 경고 로그 추가 검토 |

---

## 8. 교훈

### 잘된 점

- **Notifier 인터페이스 패턴**: WebhookNotifier, DMNotifier, LegacyNotifier를 동일 인터페이스로 추상화하여 테스트 mock 주입이 용이했다. executeSmartNotification()에서 `newNotifier(config)`만 호출하면 모드 전환이 자동으로 처리된다.
- **ProjectID 중복 제거**: 설계에서 `ClassifiedMR.ProjectID`와 `MergeRequestWithApprovals.ProjectID`를 별도로 유지하려 했으나, 구현 과정에서 중복임을 발견하고 즉시 제거했다. 설계 단계에서 데이터 흐름을 더 세밀하게 추적했다면 초기부터 제거할 수 있었다.
- **레거시 모드 하위 호환**: `config.Notification == nil` 분기로 기존 동작을 100% 보존했다. 신규 설정 없으면 코드 경로 자체가 분리되어 레거시 사용자에게 영향 없다.
- **Gap Analysis 1 Iteration 완료**: 초기 87.6%에서 재분석 후 98.2%로 개선. 누락 테스트 8건을 1회 Iteration으로 모두 보완했다.

### 개선할 점

- **우선순위 논리 결함 설계 단계 미탐지**: `blocking_discussions > changes_requested` 순서에서 `BlockingDiscussionsResolved == false` 조건이 changes_requested를 도달 불가능하게 만드는 결함이 설계 검토 단계에서 발견되지 않았다. 구현 중에 발견되어 우선순위를 교체했고 설계 문서도 사후 업데이트했다. 분류 로직 설계 시 각 우선순위 조건이 하위 케이스를 소비하는지 명시적으로 검토해야 한다.
- **통합 테스트 계획 선행 누락**: main_test.go 5건의 통합 테스트가 초기 구현에서 빠졌다가 Gap Analysis 후 추가되었다. 설계 단계에서 테스트 케이스를 목록화했음에도 구현 완료 후 확인하지 않았다. 구현 스텝마다 해당 설계 테스트 케이스를 체크리스트로 확인하는 습관이 필요하다.
- **DM 모드 execute-level 통합 테스트**: DMNotifier 단위 테스트는 충분하나, execute() 레벨에서 DM 모드가 올바르게 선택되는지 검증하는 통합 테스트가 부재하다. Webhook 모드와 동일한 수준의 통합 테스트가 있었다면 매칭율 98.2%가 아닌 100%에 도달했을 것이다.

### 다음에 시도할 것

- **설계 시 우선순위 조건 충돌 검증**: 상태 분류처럼 우선순위 기반 분기가 있을 때, 설계 문서에 각 케이스별 진입 조건 집합을 명시하고 겹침/도달 불가 여부를 표로 검증한다.
- **테스트 케이스 체크리스트 운용**: 설계 단계에서 정의한 테스트 케이스 목록을 구현 로그에 복사하고, 각 케이스를 구현 완료 직후 체크한다. Gap Analysis 전에 자체 점검으로 누락을 방지한다.
- **scheduled-summary 기능 구현 시 StateStore 인터페이스 도입**: 현재 JSON 파일 직접 구현을 StateStore 인터페이스로 감싸고, Lambda/K8s 환경에서도 상태를 유지할 수 있도록 DynamoDB 구현체를 추가한다.
