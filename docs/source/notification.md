# notification.go

## 모듈 목적

GitLab MR 분류 결과를 바탕으로 Slack 알림 대상자를 결정하고, 전송할 메시지를 구성하는 모듈.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `NotificationTarget` | 알림 대상 1명 (Slack ID, GitLab username, 역할) |
| `Notification` | 하나의 MR에 대한 알림 (분류된 MR + 대상자 목록 + 메시지) |

## 주요 함수

### `authorName(mr) string` / `authorUsername(mr) string`

`*gitlab.MergeRequest`에서 작성자 표시 이름/username을 꺼내는 nil-가드
헬퍼. `mr`이 `nil`이거나 `mr.Author`가 `nil`(삭제된 사용자, 토큰이 조회할
수 없는 서비스 계정 등)이면 각각 `"unknown"`/`""`을 반환해, 호출부가
매번 `mr.Author == nil`을 따로 체크하지 않고도 안전하게 역참조할 수 있게
한다. `resolveNotificationTargets`와 `main.go`/`slack.go`의 메시지 포맷
함수들이 공통으로 사용한다.

### `resolveNotificationTargets(classified, mapping) []*Notification`
- 분류된 MR 목록과 사용자 매핑 테이블을 받아 Notification 목록 생성
- TargetRole에 따라 Author 또는 Reviewer의 GitLab username을 Slack ID로 변환
- 매핑에 없는 사용자는 건너뛰되, mapping이 비어있지 않으면 경고 로그 출력
- author username을 `authorUsername`으로 해석할 수 없으면(빈 문자열) 경고
  로그만 남기고 작성자 알림을 건너뛴다.
- **대상은 Slack ID 기준으로 중복 제거된다.** 같은 사람이 한 MR의 작성자이자
  지정된 리뷰어이기도 한 경우, 예전에는 `Targets`에 두 번(author 역할과
  reviewer 역할로) 추가되어 멘션이 중복되고 DM에 같은 MR이 두 번 나열됐다.
  지금은 MR 하나를 처리하는 동안 이미 추가한 Slack ID를 `seen` 맵으로
  기억해뒀다가 같은 ID가 다시 나오면 건너뛴다(첫 번째로 매칭된 역할만 남음).

### `formatMentions(targets) string`
- NotificationTarget 목록을 Slack mention 문자열(`<@U123> <@U456>`)로 변환

## 의존 관계

- `classify.go`: `MRStatus`, `TargetRole`, `ClassifiedMR`, `RoleAuthor`, `RoleReviewer`, status 상수들
- `gitlab.go`: `MergeRequestWithApprovals`
- `config.go`: `UserMappingEntry`

## 데이터 흐름

```
classifyMergeRequests() -> []*ClassifiedMR
                                |
                                v
resolveNotificationTargets(classified, userMapping) -> []*Notification
                                    (Slack ID 기준 중복 제거)
                                                            |
                                                            v
                                            formatMentions(targets) -> Slack mention string
```
