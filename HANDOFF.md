# 인수인계 문서 — mr-noti-bot

> 최종 갱신: 2026-04-19 (2차)

---

## 세션 요약

이 세션에서 다음을 완료했다:
1. `smart-notification` 기능 PDCA 사이클 (이전 세션)
2. `scheduled-summary` 기능 PDCA 사이클 — Agent Teams(CTO-Led) 최초 사용
3. Go 1.26.2 설치 후 빌드/테스트 검증 — **82개 테스트 전부 통과**
4. 테스트 파일 nil pointer panic 버그 수정

## 완료된 작업

### smart-notification (PDCA 완료, 아카이브됨)

MR 상태 기반 스마트 알림. 상태 변경 시에만 해당 작성자/리뷰어에게 알림.
- 아카이브: `docs/archive/2026-04/smart-notification/`
- Match Rate: 98.2%

### scheduled-summary (PDCA 완료, 아카이브됨)

특정 시간에 전체 MR 현황 요약 전송. smart-notification과 상호 보완.
- 별도 `summary.schedule` cron, 상태별 그룹핑, stale MR 강조(7일+)
- 40 MR 초과 시 자동 분할, Korean 상대 시간 표시
- 아카이브: `docs/archive/2026-04/scheduled-summary/`
- Match Rate: 98.2% (1회 구현 PASS, Act 반복 0회)

### 빌드/테스트 검증 + 버그 수정

- Go 1.26.2 설치됨 (`/usr/local/go/bin` 경로)
- `go build ./...` 성공
- `go test ./...` — **82개 테스트 전부 통과**
- 버그 발견/수정: `notification_test.go`의 `log.SetOutput(nil)`이 후속 테스트 panic 유발
  → `log.SetOutput(os.Stderr)`로 변경

### reviewer-filter (smart-notification에 흡수)

## 커밋 (이 세션 누적, origin/main 반영 완료)

- `2bf72a1` fix(test): restore log output to os.Stderr instead of nil
- `beaf804` chore: archive scheduled-summary PDCA documents
- `1651dfe` docs: add PDCA documents for scheduled-summary feature
- `22a9851` feat: add scheduled summary for periodic MR status reports

## Agent Teams 첫 사용 관찰

- TeamCreate + SendMessage로 3인 병렬 (config-dev, summary-dev, main-dev)
- 독립 가능한 파일 단위 작업에 효과적 (~7분 내 완료)
- 중복 완료 알림 등 통신 오버헤드는 존재하나 허용 범위
- 팀원에게 "완료했으니 대기" 같은 추가 메시지를 보내지 말 것 — 재응답 루프 유발

## 미완료 / 다음 작업 후보

- **Webhook 대신 Block Kit** 사용 — Slack 메시지 가독성 향상
- **HTTP 서버 모드** — GitLab webhook 수신으로 이벤트 기반 즉시 알림 지원
- **40/80 경계 테스트** 추가 — scheduled-summary 분할 로직 엣지 보강
- **executeSummary 통합 테스트** — Slack API mock
- **WebhookNotifier 리팩토링** — `slack.PostWebhook` 직접 호출을 인터페이스로 추상화해 mock 가능하게

## 주요 설계 결정 (누적)

| 결정 | 이유 |
|------|------|
| 분류 우선순위: conflicts > changes_requested > blocking > approved > needs_review | 원래 설계의 unreachable 조건 수정 |
| Notifier 인터페이스 패턴 | mock 테스트, 모드 추가 용이 |
| JSON 파일 상태 저장 (인터페이스 아님) | Docker standalone, YAGNI |
| 배포: Docker standalone + go-quartz 내장 cron | Volume 마운트로 상태 유지 |
| scheduled-summary는 상태 파일 미사용 | 중복 방지 로직과 분리 |
| summary webhook URL 폴백 | `summary.webhook_url` → `notification.webhook.url` |
| stale 기준 7일 | 주간 리뷰 사이클 기준 |
| 테스트에서 `log.SetOutput` 복원은 `os.Stderr` | `nil` 복원은 후속 테스트 panic 유발 |

## 설정 변경 누적

`config.yaml.example`에 추가된 항목:
- `notification` (mode, webhook, bot)
- `user_mapping` (gitlab_username → slack_id)
- `state` (path)
- `summary` (schedule, webhook_url, stale_days)

## 알려진 이슈 (업데이트)

- ~~Go가 WSL2 환경에 미설치~~ → **해결** (Go 1.26.2 설치, 82개 테스트 통과)
- WebhookNotifier.Send의 `slack.PostWebhook` 직접 호출은 mock 불가 (smart-notification 기술 부채, 미해결)
- scheduled-summary의 스케줄러 에러 로그 문구가 설계와 미미하게 다름 (기능 동일, cosmetic)

## Go 환경 참고

- 바이너리: `/usr/local/go/bin/go` (PATH에 포함 필요)
- 버전: go1.26.2 linux/amd64
- 빌드 명령: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- 테스트 명령: `export PATH=$PATH:/usr/local/go/bin && go test ./...`
