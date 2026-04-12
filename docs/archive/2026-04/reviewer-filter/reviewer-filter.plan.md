# Plan: 작성자/리뷰어 필터링 기능 추가

## Executive Summary

| 항목 | 내용 |
|------|------|
| Feature | reviewer-filter |
| 작성일 | 2026-04-12 |
| 예상 규모 | Medium (기능 추가) |

### Value Delivered

| 관점 | 내용 |
|------|------|
| **Problem** | 현재 MR 작성자(author)로만 필터링 가능하여, 리뷰어로 배정된 MR을 추적할 수 없다 |
| **Solution** | `reviewers` 설정 항목을 추가하고, MR의 reviewer 필드를 기준으로 필터링하는 로직을 구현한다 |
| **Function UX Effect** | 사용자가 자신이 리뷰해야 할 MR 알림을 Slack으로 받아 리뷰 누락을 방지한다 |
| **Core Value** | 코드 리뷰 응답 속도 개선 및 MR 체류 시간 단축 |

---

## 1. 배경 및 동기

현재 `mr-noti-bot`은 열린 MR을 조회한 뒤 `authors` 설정으로 **작성자 기준** 필터링만 지원한다.

실제 워크플로우에서는:
- **작성자**: 리뷰 피드백을 받았는지 확인 필요
- **리뷰어**: 자신에게 배정된 MR을 리뷰해야 함

리뷰어 필터링이 없으면 리뷰 배정을 놓치기 쉽고, MR이 장시간 방치될 수 있다.

## 2. 목표

1. MR의 `reviewers` 필드를 기준으로 필터링하는 기능 추가
2. 기존 `authors` 필터와 독립적으로 또는 함께 사용 가능
3. YAML 설정 파일 및 환경변수 모두 지원
4. 기존 동작(하위 호환성) 유지

## 3. 현재 구조 분석

### 3.1 설정 흐름
```
config.yaml / 환경변수
  → loadConfig() (config.go)
    → Config 구조체
```

### 3.2 실행 흐름
```
execute()
  → fetchOpenedMergeRequests()  ← GitLab API 호출
  → filterMergeRequestsByAuthor()  ← 작성자 필터 (유일한 필터)
  → formatMergeRequestsSummary()
  → sendSlackMessage()
```

### 3.3 현재 필터 동작
- `Config.Authors []ConfigAuthor` — ID 또는 Username으로 MR 작성자 매칭
- `authors`가 비어있으면 전체 MR 통과 (필터 없음)

### 3.4 go-gitlab MergeRequest 구조체
- `Author *BasicUser` — MR 작성자 (현재 사용 중)
- `Reviewers []*BasicUser` — MR 리뷰어 목록 (GitLab API v4 지원, 활용 예정)

## 4. 변경 계획

### 4.1 설정 확장 (config.go)

**Config 구조체에 `Reviewers` 필드 추가:**

```go
type Config struct {
    // ... 기존 필드
    Authors   []ConfigAuthor `yaml:"authors"`
    Reviewers []ConfigAuthor `yaml:"reviewers"`  // 신규
}
```

- `ConfigAuthor` 구조체 재활용 (ID/Username 동일 구조)
- 환경변수: `REVIEWERS` (쉼표 구분, authors와 동일 파싱 방식)

**config.yaml 예시:**
```yaml
authors:
  - username: "janedoe"
reviewers:
  - username: "johndoe"
  - id: 918
```

### 4.2 필터링 로직 추가 (main.go)

**새 함수 `filterMergeRequestsByReviewer` 추가:**

```
filterMergeRequestsByReviewer(mrs, reviewers) → []*MergeRequestWithApprovals
```

- MR의 `Reviewers` 필드에서 설정된 사용자 매칭
- `reviewers` 비어있으면 전체 통과 (기존 authors 동작과 동일)

**execute() 흐름 변경:**

```
현재: fetchMRs → filterByAuthor → format → send
변경: fetchMRs → filterByAuthor → filterByReviewer → format → send
```

> **주의**: 두 필터는 **교집합** 방식으로 동작.
> - `authors`만 설정 → 작성자 기준 필터
> - `reviewers`만 설정 → 리뷰어 기준 필터
> - 둘 다 설정 → 작성자 AND 리뷰어 조건 모두 충족하는 MR만 통과
> - 둘 다 미설정 → 전체 MR 통과 (기존 동작)

### 4.3 Slack 메시지 포매팅 (main.go)

`formatMergeRequestsSummary`에 리뷰어 정보 추가 표시:

```
:arrow_forward: <MR URL|MR Title>
*Author:* John Doe
*Reviewers:* Jane Doe, Bob Smith    ← 신규
*Created at:* 12 April 2026, 10:00 UTC
*Approved by:* Jane Doe
```

- `MergeRequest.Reviewers` 필드에서 이름 목록 추출
- 리뷰어가 없으면 "None" 표시

### 4.4 수정 대상 파일

| 파일 | 변경 내용 |
|------|-----------|
| `config.go` | `Config.Reviewers` 필드 추가, `loadConfig()`에 `REVIEWERS` 환경변수 파싱 추가 |
| `config_test.go` | Reviewers 설정 로드 테스트 추가 |
| `main.go` | `filterMergeRequestsByReviewer()` 함수 추가, `execute()` 흐름에 리뷰어 필터 삽입, `formatMergeRequestsSummary()` 리뷰어 표시 |
| `main_test.go` | 리뷰어 필터 단위 테스트 추가 |
| `config.yaml.example` | `reviewers` 설정 예시 추가 |
| `config.test.yaml` | 테스트용 reviewers 설정 추가 |
| `README.md` | reviewers 설정 문서화 |

## 5. 구현 순서

1. `config.go` — `Reviewers` 필드 및 환경변수 파싱
2. `config_test.go` — 설정 로드 테스트
3. `main.go` — `filterMergeRequestsByReviewer()` 함수
4. `main.go` — `formatMergeRequestsSummary()` 리뷰어 표시
5. `main.go` — `execute()` 파이프라인에 필터 삽입
6. `main_test.go` — 필터 함수 테스트
7. `config.yaml.example` / `config.test.yaml` 갱신
8. `README.md` 문서 갱신

## 6. 범위 외 (Out of Scope)

- 작성자/리뷰어별 개별 Slack 채널 전송 (별도 기능)
- 리뷰어 자동 배정 기능
- Slack DM 개인 알림
- `OR` 조합 모드 (authors 또는 reviewers) — 향후 확장 가능

## 7. 위험 및 고려사항

| 위험 | 대응 |
|------|------|
| go-gitlab v0.107.0에서 `MergeRequest.Reviewers` 필드 존재 여부 | GitLab API v4에서 reviewers는 지원됨, 라이브러리 버전 확인 필요 |
| 기존 사용자 하위 호환성 | `reviewers` 미설정 시 기존과 동일하게 동작 (필터 스킵) |
| 두 필터 조합 시 의도치 않은 빈 결과 | 문서에 AND 동작 명시, 향후 OR 모드 확장 고려 |
