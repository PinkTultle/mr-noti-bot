# notification.go

## 모듈 목적

GitLab MR 분류 결과를 바탕으로 Slack 알림 대상자를 결정하고, 전송할 메시지를 구성하는 모듈.

## 주요 타입

| 타입 | 역할 |
|------|------|
| `NotificationTarget` | 알림 대상 1명 (Slack ID, GitLab username, 역할) |
| `Notification` | 하나의 MR에 대한 알림 (분류된 MR + 대상자 목록 + 메시지) |

## 주요 함수

### `resolveNotificationTargets(classified, mapping) []*Notification`
- 분류된 MR 목록과 사용자 매핑 테이블을 받아 Notification 목록 생성
- TargetRole에 따라 Author 또는 Reviewer의 GitLab username을 Slack ID로 변환
- 매핑에 없는 사용자는 건너뛰되, mapping이 비어있지 않으면 경고 로그 출력

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
                                                            |
                                                            v
                                            formatMentions(targets) -> Slack mention string
```
