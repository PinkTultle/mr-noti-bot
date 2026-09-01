# gitlab.go -- GitLab API 클라이언트 및 MR 수집

## 모듈 목적

GitLab API 호출을 인터페이스 뒤로 감추고, 설정된 그룹/서브그룹/프로젝트를 전개하여
열린 MR과 각 MR의 승인 정보를 모아 `MergeRequestWithApprovals` 목록으로 반환한다.
`classify.go` 이후의 파이프라인 전체가 소비하는 데이터를 만들어내는 유일한 지점이다.

## 타입 정의

### GitLabClient

GitLab API 호출을 추상화하는 인터페이스. 메서드 4개만 노출한다:
`ListGroupProjects`, `ListSubGroups`, `ListProjectMergeRequests`,
`GetMergeRequestApprovalsConfiguration`. 실제 구현체는 `gitLabClient`이고,
테스트에서는 `mocks/`의 mockery 생성 mock으로 대체한다
(`//go:generate mockery --name GitLabClient`).

### MergeRequestWithApprovals

| 필드 | 타입 | 설명 |
|------|------|------|
| `MergeRequest` | `*gitlab.MergeRequest` | go-gitlab 원본 MR 객체 |
| `ApprovedBy` | `[]string` | 승인자 이름 목록 |
| `ProjectID` | `int` | MR이 속한 프로젝트 ID |

### gitLabClient

`GitLabClient`의 실제 구현체. `github.com/xanzy/go-gitlab`의 `*gitlab.Client`를
감싸 각 메서드를 그대로 위임한다.

## 주요 함수

### `newGitLabClient(config) -> (GitLabClient, error)`

`config.GitLab.Token`/`URL`로 `gitlab.NewClient`를 생성하고 `gitLabClient`로
감싸 반환하는 헬퍼. `main.go`의 `execute`와 `summary.go`의 `executeSummary`가
모두 이 함수를 통해서만 클라이언트를 만든다 — 생성 로직이 한 곳에만 있어
GitLab 클라이언트 구성을 바꿀 때 여러 진입점을 따로 고칠 필요가 없다.

### `fetchOpenedMergeRequests(config, client) -> ([]*MergeRequestWithApprovals, error)`

전체 파이프라인의 시작점. 처리 순서:

1. `config.Groups`의 각 그룹에 대해 `fetchSubGroups`로 하위그룹 ID를 전개한다.
2. 원본 그룹 ID + 하위그룹 ID를 `dedupeIDs`로 중복 제거한다.
3. 그 그룹 ID들로부터 `fetchProjectsFromGroups`로 프로젝트 ID를 모은다.
4. `config.Projects`에 직접 지정된 프로젝트 ID를 더한다.
5. 전체 프로젝트 ID 목록을 다시 `dedupeIDs`로 중복 제거한다.
6. 프로젝트마다 `opened` 상태 MR을 페이지네이션(`PerPage: 50`)으로 전부 가져오고,
   MR 하나당 `GetMergeRequestApprovalsConfiguration`을 별도로 호출해 승인자
   이름을 채워 `MergeRequestWithApprovals`를 만든다.

### `dedupeIDs(ids) -> []int`

순서를 보존하며 중복 정수를 제거한다. 어떤 프로젝트가 그룹을 통해서도, `projects`
목록에도 동시에 지정돼 있으면 이 함수가 없을 때 두 번 순회되어 알림이 중복되고
승인 조회 API 호출도 그만큼 늘어난다.

### `fetchProjectsFromGroups(groupIDs, client)` / `fetchSubGroups(groupID, client)`

각각 `ListGroupProjects` / `ListSubGroups`를 페이지네이션하며 ID만 뽑아내는 헬퍼.
`fetchSubGroups`는 그룹 하나에 대해 1회만 호출되며 재귀하지 않는다 — 서브그룹의
서브그룹까지는 전개하지 않는다.

## 의존 관계

- `github.com/xanzy/go-gitlab`: API 타입과 클라이언트
- `config.go`: `Config`, `ConfigGroup`, `ConfigProject`
- `classify.go`, `notification.go`, `state.go`, `summary.go`: 이 파일이 만드는
  `MergeRequestWithApprovals`를 소비

## 주의사항

- **N+1 API 호출**: 프로젝트당 MR 목록 조회는 1회지만, 승인 정보는 MR마다
  별도 호출이다. 구독한 그룹/프로젝트에 열린 MR이 많을수록 폴링 주기당 API
  호출 수가 MR 수에 비례해 늘어난다 — GitLab rate limit을 고려해야 한다.
- 서브그룹 전개는 1단계뿐이다.
- Draft(WIP) MR은 `WIP: "no"` 옵션으로 조회에서 제외된다.
- 종료 조건은 `resp.CurrentPage >= resp.TotalPages`이므로 결과가 없는 빈
  페이지(`TotalPages == 0`)에서도 안전하게 루프가 끝난다.
