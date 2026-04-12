# classify.go -- MR 상태 분류 엔진

## 모듈 목적

GitLab MR의 현재 상태(충돌, 디스커션, 승인 등)를 분석하여 5가지 상태 중 하나로 분류하고,
각 상태에서 행동해야 할 대상 역할(author/reviewer)을 결정한다.

## 타입 정의

### MRStatus

MR의 워크플로우 상태를 나타내는 문자열 상수:

| 상수 | 값 | 의미 |
|------|-----|------|
| `StatusNeedsReview` | `needs_review` | 리뷰 대기 |
| `StatusChangesRequested` | `changes_requested` | 변경 요청됨 |
| `StatusApprovedPendingMerge` | `approved_pending_merge` | 승인됨, 머지 대기 |
| `StatusHasConflicts` | `has_conflicts` | 충돌 발생 |
| `StatusBlockingDiscussions` | `blocking_discussions` | 미해결 블로킹 디스커션 |

### TargetRole

알림 대상 역할: `RoleAuthor` (작성자), `RoleReviewer` (리뷰어)

### ClassifiedMR

분류 결과를 담는 구조체. 원본 MR 참조 + 상태 + 대상 역할 목록.

## 주요 함수

### classifyMergeRequest(mr) -> *ClassifiedMR

단일 MR을 분류한다. 우선순위 기반으로 첫 매칭 상태를 반환:

1. 충돌(HasConflicts) -> has_conflicts, [author]
2. 부분 승인(ApprovedBy > 0 && < Reviewers) -> changes_requested, [author]
3. 블로킹 디스커션 미해결 -> blocking_discussions, [author, reviewer]
4. 승인됨 + 디스커션 해결 -> approved_pending_merge, [author]
5. 승인 0건 -> needs_review, [reviewer]

### classifyMergeRequests(mrs) -> []*ClassifiedMR

배치 분류. 입력 슬라이스를 순회하며 classifyMergeRequest를 호출한다.

## 의존 관계

- `gitlab.go`의 `MergeRequestWithApprovals` 구조체
- `github.com/xanzy/go-gitlab` (MergeRequest의 HasConflicts, BlockingDiscussionsResolved, Reviewers 필드)

## 주의사항

- 원래 설계에서 blocking_discussions가 changes_requested보다 우선이었으나,
  그 순서에서는 `!BlockingDiscussionsResolved` 조건이 이미 Priority 2에서 소비되어
  changes_requested가 도달 불가능했다. 구현 시 changes_requested를 Priority 2로 이동하고
  `!BlockingDiscussionsResolved` 조건을 제거하여 해결했다.
  설계 문서(Section 2.2)도 이에 맞게 갱신됨.
