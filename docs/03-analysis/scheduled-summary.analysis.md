# Gap Analysis: scheduled-summary

- **Date**: 2026-04-19
- **Match Rate**: 98.2%
- **Verdict**: PASS (proceed to Report stage)

---

## 1. Section 2 — Config Extension (`config.go`) — 5/5 PASS

| # | Design Item | Code | Verdict |
|---|-------------|------|---------|
| 2.1 | `SummaryConfig` struct (Schedule/WebhookURL/StaleDays with yaml tags) | `config.go:66-70` | PASS |
| 2.2 | `Config.Summary *SummaryConfig` | `config.go:27` | PASS |
| 2.3 | `SUMMARY_SCHEDULE` env parsing | `config.go:177-182` | PASS |
| 2.3 | `SUMMARY_WEBHOOK_URL` env parsing | `config.go:184-189` | PASS |
| 2.3 | `SUMMARY_STALE_DAYS` numeric parsing + error | `config.go:191-200` | PASS |

## 2. Section 3 — Summary Module (`summary.go`) — 20/20 PASS

모든 헬퍼 7종 (`resolveSummaryWebhookURL`, `resolveStaleDays`, `formatRelativeTime`, `isStale`, `groupClassifiedByStatus`, `formatMRLine`, `formatStatusSection`), `formatSummaryHeader`, `formatEmptySummary`, `formatSummaryMessages`, `executeSummary`이 설계대로 구현됨.

- 긴급도 순서 (Conflicts → Blocking → Changes → Approved → NeedsReview) 일치
- 5개 상태 라벨 일치 (:warning:, :no_entry_sign:, :pencil2:, :white_check_mark:, :eyes:)
- 분할 로직: 40개 초과 시 상태 섹션 유지, 단일 상태가 40 초과 시 재분할
- Stale MR `:exclamation:` 접두사
- 빈 그룹 섹션 스킵

## 3. Section 4 — Scheduler (`main.go`) — 8/8 PASS (2 PARTIAL — 로그 문구만 미미한 차이)

| # | Design Item | Code | Verdict |
|---|-------------|------|---------|
| 4.1 | `runScheduler` 리팩토링 | `main.go:60-82` | PASS |
| 4.1 | `registerExecuteJob` 헬퍼 | `main.go:84-97` | PARTIAL (로그 문구 `"Error during scheduled execution"` vs `"Error during execute"`) |
| 4.1 | `registerSummaryJob` 헬퍼 | `main.go:99-112` | PARTIAL (로그 문구 `"Error during summary execution"` vs `"Error during summary"`) |
| 4.1 | JobKey `"executeJob"`, `"summaryJob"` | `main.go:96, 111` | PASS |
| 4.2 | `shouldRunOneShot` 헬퍼로 추출 | `main.go:45-53` | PASS (테스트 가능성 향상) |

## 4. Section 5.1 — Summary Tests (17/17 PASS)

| # | 테스트 | 구현 |
|---|--------|------|
| 1-6 | FormatSummaryMessages 시나리오 (Empty, SingleStatus, AllStatuses, EmptyGroups, Stale, Split) | `summary_test.go:29-173` |
| 7 | GroupClassifiedByStatus | `summary_test.go:175-188` |
| 8-10 | FormatRelativeTime (Days, Hours, JustNow) | `summary_test.go:190-206` |
| 11-12 | IsStale (True, False) | `summary_test.go:208-220` |
| 13-15 | ResolveSummaryWebhookURL (Explicit, Fallback, Empty) | `summary_test.go:222-245` |
| 16-17 | ResolveStaleDays (Explicit, Default) | `summary_test.go:247-258` |

## 5. Section 5.2 — Config Tests (2/2 PASS)

- summary 환경변수 정상 파싱 `config_test.go:95-111`
- SUMMARY_STALE_DAYS 에러 `config_test.go:113-124`

## 6. Section 5.3 — Main Tests (BONUS PASS)

선택 항목이었으나 `TestShouldRunOneShot` 6-case 테이블 테스트 구현됨 (`main_test.go:394-437`).

## 7. Section 6.1 — config.yaml.example (4/4 PASS)

`summary:` 블록 추가. `schedule`, 주석 처리된 `webhook_url`/`stale_days` 모두 포함.

## 8. 의도적 변경 (정당화됨)

1. `shouldRunOneShot` 헬퍼 추출 — 테스트 용이성 향상
2. `isStale` nil 가드 추가 — 방어 코드
3. `groupClassifiedByStatus` nil-entry 필터 — 방어 코드
4. 스케줄러 에러 로그 문구 미미한 차이 — 기능 동일
5. `formatRelativeTime` 내부 계산 단순화 — 출력 동일
6. `config.yaml.example` webhook_url 주석에 폴백 설명 추가

## 9. 미구현 — 없음

## 10. Minor (비차단)

- Split 로직: 40 / 41 / 80 경계 케이스 중 45만 테스트. 구현은 정확하지만 향후 회귀 위험 대비 경계 테스트 추가 권장.
- `executeSummary` 통합 테스트 없음 — 재사용 컴포넌트들이 이미 테스트됨, Slack API는 외부 의존이라 허용 범위.

## 11. Match Rate

| Section | Items | PASS | PARTIAL | FAIL |
|---------|-------|------|---------|------|
| 2. Config | 5 | 5 | 0 | 0 |
| 3. Summary module | 20 | 20 | 0 | 0 |
| 4. Scheduler | 8 | 6 | 2 | 0 |
| 5.1 Summary tests | 17 | 17 | 0 | 0 |
| 5.2 Config tests | 2 | 2 | 0 | 0 |
| 5.3 Main tests | 1 | 1 | 0 | 0 |
| 6.1 config.yaml.example | 4 | 4 | 0 | 0 |
| **Total** | **57** | **55** | **2** | **0** |

**Match Rate** = (55 + 2×0.5) / 57 = **98.2%**

## 12. Verdict

**PASS** — Match rate 98.2% (> 90%). Report 단계로 진행.

선택 후속작업:
- 40/80 경계 테스트 추가 (낮은 우선순위)
- 스케줄러 에러 로그 문구 설계와 정확히 일치시키기 (미미한 영향)
