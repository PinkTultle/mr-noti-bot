# state.go -- 알림 중복 제거 상태 관리

## 모듈 목적

폴링 주기 간 알림 중복 전송을 방지하기 위한 상태 관리 모듈이다.
이전에 전송한 알림의 MR별 상태를 JSON 파일로 저장/로드하고,
현재 분류 결과와 비교하여 상태가 변경된 MR만 필터링한다.

## 타입 정의

### NotificationState

전체 알림 상태를 보관하는 최상위 구조체.

| 필드 | 타입 | 설명 |
|------|------|------|
| `Notifications` | `map[string]MRNotificationRecord` | MR별 알림 기록. 키는 `"ProjectID:MR_IID"` 형식 |
| `UpdatedAt` | `time.Time` | 상태 파일 마지막 갱신 시각 |

### MRNotificationRecord

개별 MR의 마지막 알림 기록.

| 필드 | 타입 | 설명 |
|------|------|------|
| `Status` | `MRStatus` | 마지막 알림 시점의 MR 분류 상태 |
| `NotifiedAt` | `time.Time` | 알림 전송 시각 |

## 주요 함수

### stateKey(projectID, mrIID) -> string

MR의 고유 키를 생성한다. `"ProjectID:MR_IID"` 형식 (예: `"123:1001"`).
프로젝트가 다르면 같은 IID라도 다른 키가 된다.

### loadState(path) -> (*NotificationState, error)

JSON 파일에서 상태를 로드한다.
- `path`가 빈 문자열이면 빈 상태를 반환한다 (상태 비활성화).
- 파일이 존재하지 않으면 빈 상태를 반환한다 (첫 실행).
- JSON 파싱 실패 시 에러를 반환한다.
- `Notifications` 맵이 null인 경우 빈 맵으로 초기화한다.

### saveState(path, state) -> error

상태를 JSON 파일에 저장한다.
- `path`가 빈 문자열이면 아무 작업도 하지 않는다.
- 저장 시 `UpdatedAt`을 현재 시각으로 갱신한다.

### filterChangedMRs(classified, prevState) -> []*ClassifiedMR

분류된 MR 목록을 이전 상태와 비교하여 새로 등장하거나 상태가 변경된 MR만 반환한다.
이전 상태에 키가 없거나 상태값이 다르면 변경된 것으로 판단한다.

### buildNewState(classified) -> *NotificationState

현재 분류된 MR 목록으로 새 상태를 구성한다.
이전 상태에 있었으나 현재 목록에 없는 MR은 자동으로 제거된다 (stale cleanup).

## 의존 관계

- `classify.go`의 `ClassifiedMR`, `MRStatus` 타입
- `gitlab.go`의 `MergeRequestWithApprovals` 구조체
- 표준 라이브러리: `encoding/json`, `fmt`, `os`, `time`

## 주의사항

- 상태 파일 경로가 빈 문자열이면 중복 제거가 비활성화되어 매 폴링마다 전체 알림을 전송한다.
- 상태 파일은 프로세스 간 공유되지 않는다. 단일 인스턴스 실행을 전제한다.
- `buildNewState`는 현재 목록만으로 상태를 구성하므로, 닫힌 MR은 자연스럽게 제거된다.
