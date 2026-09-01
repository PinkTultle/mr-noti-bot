# classify.go -- MR 상태 분류 엔진

## 모듈 목적

GitLab MR의 현재 상태(충돌, 디스커션, 승인)를 분석하여 상태 하나로 분류하고,
그 상태에서 행동해야 할 대상 역할(author/reviewer)을 결정한다.

## 타입 정의

### MRStatus

MR의 워크플로우 상태를 나타내는 문자열 상수:

| 상수 | 값 | 의미 |
|------|-----|------|
| `StatusNeedsReview` | `needs_review` | 리뷰(또는 남은 리뷰어의 승인) 대기 |
| `StatusChangesRequested` | `changes_requested` | (더 이상 생성되지 않음 — 아래 주의사항 참고) |
| `StatusApprovedPendingMerge` | `approved_pending_merge` | 승인됨, 머지 대기 |
| `StatusHasConflicts` | `has_conflicts` | 충돌 발생 |
| `StatusBlockingDiscussions` | `blocking_discussions` | 미해결 블로킹 디스커션 |

### TargetRole

알림 대상 역할: `RoleAuthor`(작성자), `RoleReviewer`(리뷰어)

### ClassifiedMR

분류 결과를 담는 구조체. 원본 MR 참조 + 상태 + 대상 역할 목록.

## 주요 함수

### `classifyMergeRequest(mr) -> *ClassifiedMR`

단일 MR을 분류한다. 우선순위 기반으로 첫 매칭 규칙이 이긴다:

1. 충돌(`HasConflicts`) → `has_conflicts`, `[author]`
2. 미해결 블로킹 디스커션(`!BlockingDiscussionsResolved`) → `blocking_discussions`, `[author, reviewer]`
3. 승인자가 1명 이상(`len(ApprovedBy) > 0`)일 때:
   - 지정된 리뷰어가 없거나(`reviewerCount == 0`) 승인자 수가 리뷰어 수 이상이면
     → `approved_pending_merge`, `[author]` (전원 승인 또는 승인 필요 조건 충족으로 간주)
   - 그렇지 않으면(부분 승인) → `needs_review`, `[reviewer]` (아직 승인하지 않은 리뷰어를 기다림)
4. 승인 0건 → `needs_review`, `[reviewer]`

### `classifyMergeRequests(mrs) -> []*ClassifiedMR`

배치 분류. 입력 슬라이스를 순회하며 `classifyMergeRequest`를 호출한다.

## 의존 관계

- `gitlab.go`의 `MergeRequestWithApprovals` 구조체
- `github.com/xanzy/go-gitlab` (`MergeRequest`의 `HasConflicts`,
  `BlockingDiscussionsResolved`, `Reviewers` 필드)

## 주의사항

- **`StatusChangesRequested`는 더 이상 생성되지 않는다.** 원래는 "일부만
  승인됨"을 changes_requested로 분류했으나, 그 판단은 실제로는 "아직 승인
  안 한 리뷰어가 남아있다"는 뜻일 뿐 변경 요청 여부와는 무관했다(GitLab
  MR 자체에 "changes requested"라는 명시적 상태가 없다 — 이 상수는 앱이
  임의로 붙인 해석이었다). 지금은 부분 승인을 `needs_review` + 리뷰어 대상
  알림으로 분류해, 아직 응답하지 않은 리뷰어에게 다시 알리는 방식으로
  처리한다. `StatusChangesRequested` 상수와 `statusMessages`/
  `summaryStatusHeaders`의 대응 항목은 하위 호환(과거 상태 파일에 남아있을
  수 있는 값)을 위해 남아 있지만, `classifyMergeRequest`는 이 값을 절대
  반환하지 않는다.
- `ApprovedBy`는 승인자 "이름"이고 `Reviewers`는 지정된 리뷰어 목록이라
  두 값의 개수 비교가 항상 정확하지는 않다(동명이인, 승인 후 리뷰어에서
  제외된 경우 등). 리뷰어가 아예 지정되지 않은 MR은 승인 1건만으로도
  머지 대기로 간주한다.
- 우선순위는 고정 순서다 — 충돌과 블로킹 디스커션이 승인 여부보다 항상
  우선한다.
