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

## 에러 타입

### `ErrStateCorrupted` / `isStateCorrupted(err) bool`

상태 파일이 "존재하지만 파싱에 실패"한 경우를 나타내는 sentinel 에러다.
`loadState`는 파일이 아예 없는 경우(첫 실행)와 파일은 있으나 JSON이 깨진
경우를 구분해서 반환하며, 호출부(`main.go`의 `executeSmartNotification`)는
`isStateCorrupted`로 이 둘을 구분해 다르게 처리한다: 파일이 없으면 빈
상태로 계속 진행하지만, 손상된 파일이면 실행을 실패시켜 사람이 확인하게
한다. 손상을 빈 상태로 조용히 대체하면 열린 MR 전체가 "새로 등장한 것"으로
간주되어 한꺼번에 재알림된다 — 이를 막기 위한 구분이다.

## 주요 함수

### `stateKey(projectID, mrIID) -> string`

MR의 고유 키를 생성한다. `"ProjectID:MR_IID"` 형식 (예: `"123:1001"`).
프로젝트가 다르면 같은 IID라도 다른 키가 된다.

### `loadState(path) -> (*NotificationState, error)`

JSON 파일에서 상태를 로드한다.
- `path`가 빈 문자열이면 빈 상태를 반환한다 (상태 비활성화).
- 파일이 존재하지 않으면 빈 상태를 반환한다 (첫 실행).
- JSON 파싱 실패 시 `ErrStateCorrupted`를 감싼 에러를 반환한다.
- `Notifications` 맵이 null인 경우 빈 맵으로 초기화한다.

### `saveState(path, state) -> error`

상태를 JSON 파일에 **원자적으로** 저장한다: 같은 디렉토리에 임시 파일을
만들어 쓰고 `fsync` 후 대상 경로로 `rename`한다. 파일에 직접 덮어쓰다가
프로세스가 중단(SIGKILL, OOM, 디스크 풀 등)되면 잘린 JSON이 남고, 다음
실행이 이를 "상태 없음"으로 오인해 열린 MR 전체를 재알림하게 된다 —
temp+rename은 이 경로를 없앤다. `rename`이 성공하면 임시 파일은 이미
사라졌으므로 `defer os.Remove`는 rename 성공 시 아무 일도 하지 않는다.
- `path`가 빈 문자열이면 아무 작업도 하지 않는다.
- 저장 시 `UpdatedAt`을 현재 시각으로 갱신한다.

### `filterChangedMRs(classified, prevState) -> []*ClassifiedMR`

분류된 MR 목록을 이전 상태와 비교하여 새로 등장하거나 상태가 변경된 MR만 반환한다.
이전 상태에 키가 없거나 상태값이 다르면 변경된 것으로 판단한다.

### `buildNewState(classified) -> *NotificationState`

현재 분류된 MR 목록으로 새 상태를 구성한다.
이전 상태에 있었으나 현재 목록에 없는 MR은 자동으로 제거된다 (stale cleanup).

## 의존 관계

- `classify.go`의 `ClassifiedMR`, `MRStatus` 타입
- `gitlab.go`의 `MergeRequestWithApprovals` 구조체
- 표준 라이브러리: `encoding/json`, `errors`, `fmt`, `os`, `path/filepath`, `time`

## 주의사항

- 상태 파일 경로가 빈 문자열이면 중복 제거가 비활성화되어 매 폴링마다 전체 알림을 전송한다.
- 상태 파일은 프로세스 간 공유되지 않는다. 단일 인스턴스 실행을 전제한다.
- `buildNewState`는 현재 목록만으로 상태를 구성하므로, 닫힌 MR은 자연스럽게 제거된다.
- 원자적 쓰기는 임시 파일과 대상 파일이 같은 디렉토리(같은 파일시스템)에
  있어야 `rename`이 원자적임을 보장한다 — `os.CreateTemp(dir, ...)`가 이를
  위해 대상 경로의 디렉토리를 그대로 사용한다.
