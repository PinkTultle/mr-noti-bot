# 인수인계 문서 — mr-noti-bot

> 최종 갱신: 2026-04-12

---

## 세션 요약

smart-notification 기능의 PDCA 전체 사이클(Plan → Design → Do → Check → Act → Report → Archive)을 완료했다.

## 완료된 작업

### smart-notification (PDCA 완료, 아카이브됨)

MR 상태 기반 스마트 알림 기능 구현.

- **상태 분류**: 열린 MR을 5가지 상태로 분류 (`has_conflicts`, `changes_requested`, `blocking_discussions`, `approved_pending_merge`, `needs_review`)
- **알림 대상 결정**: 상태별로 작성자/리뷰어를 자동 결정, GitLab → Slack 사용자 매핑
- **전송 모드**: Webhook 멘션 / Bot DM / Legacy (설정으로 전환)
- **중복 방지**: JSON 상태 파일로 상태 변경 시에만 알림
- **하위 호환**: `notification` 설정 없으면 기존 동작 100% 유지

**커밋:**
- `a3a591f` feat: add smart notification with MR status-based routing
- `d5e03c1` docs: add PDCA documents for smart-notification feature
- `7f7a11c` chore: archive smart-notification and reviewer-filter PDCA documents

**Match Rate:** 98.2% (1회 반복)
**테스트:** 60개 함수
**아카이브:** `docs/archive/2026-04/smart-notification/`

### reviewer-filter (smart-notification에 흡수)

초기에 별도 기능으로 Plan/Design을 작성했으나, 상위 기능 smart-notification의 Step 1, 2에 통합됨.
아카이브: `docs/archive/2026-04/reviewer-filter/`

## 미완료 / 다음 작업

### scheduled-summary (미착수)

- **내용**: 특정 시간에 전체 MR 현황 요약을 전송하는 기능
- **의존성**: smart-notification의 `classifyMergeRequests`, `UserMapping`, `Notifier` 재활용 가능
- **시작 명령**: `/pdca plan scheduled-summary`

## 주요 설계 결정 (다음 세션 참고)

| 결정 | 이유 |
|------|------|
| 분류 우선순위: conflicts > changes_requested > blocking > approved > needs_review | 원래 설계는 blocking이 2순위였으나, changes_requested 조건이 도달 불가능해서 순서 변경 |
| Notifier 인터페이스 패턴 | mock 테스트 용이, 향후 이메일 등 모드 추가 시 구현체만 추가 |
| JSON 파일 상태 저장 (인터페이스 아님) | Docker standalone 확정, YAGNI 원칙. 향후 Lambda 필요 시 StateStore 인터페이스화 |
| 배포: Docker standalone + go-quartz 내장 cron | Volume 마운트로 상태 파일 유지. K8s CronJob/Lambda는 향후 확장 |

## 설정 변경 사항

`config.yaml.example`에 추가된 항목:
- `notification` (mode, webhook, bot)
- `user_mapping` (gitlab_username → slack_id)
- `state` (path)

## 알려진 이슈

- Go가 WSL2 환경에 미설치 → `go test ./...` 빌드 검증 미수행. 다음 세션에서 Go 환경 확보 후 테스트 실행 필요
- WebhookNotifier.Send의 `slack.PostWebhook` 직접 호출은 mock 불가 → 기술 부채로 기록됨
