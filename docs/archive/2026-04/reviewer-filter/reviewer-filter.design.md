# Design: 작성자/리뷰어 필터링 기능 추가

> Plan 참조: `docs/01-plan/features/reviewer-filter.plan.md`

---

## 1. 설계 개요

MR의 `Reviewers` 필드를 기준으로 필터링하는 기능을 추가한다.
기존 `authors` 필터와 동일한 패턴(`ConfigAuthor` 재활용, 환경변수 지원)으로 구현하여
코드 일관성을 유지한다.

### 변경 범위

```
config.go       ← Config 구조체 확장 + 환경변수 파싱
main.go         ← 필터 함수 추가 + execute 흐름 수정 + 메시지 포매팅
config_test.go  ← 설정 로드 테스트
main_test.go    ← 필터 함수 테스트
config.yaml.example / config.test.yaml ← 설정 예시
```

---

## 2. 상세 설계

### 2.1 Config 구조체 변경 — `config.go:12-24`

**현재:**
```go
type Config struct {
    GitLab struct {
        URL   string `yaml:"url"`
        Token string `yaml:"token"`
    } `yaml:"gitlab"`
    Slack struct {
        WebhookURL string `yaml:"webhook_url"`
    } `yaml:"slack"`
    Projects     []ConfigProject `yaml:"projects"`
    Groups       []ConfigGroup   `yaml:"groups"`
    CronSchedule string          `yaml:"cron_schedule"`
    Authors      []ConfigAuthor  `yaml:"authors"`
}
```

**변경 후:**
```go
type Config struct {
    GitLab struct {
        URL   string `yaml:"url"`
        Token string `yaml:"token"`
    } `yaml:"gitlab"`
    Slack struct {
        WebhookURL string `yaml:"webhook_url"`
    } `yaml:"slack"`
    Projects     []ConfigProject `yaml:"projects"`
    Groups       []ConfigGroup   `yaml:"groups"`
    CronSchedule string          `yaml:"cron_schedule"`
    Authors      []ConfigAuthor  `yaml:"authors"`
    Reviewers    []ConfigAuthor  `yaml:"reviewers"`   // 추가
}
```

- `ConfigAuthor` 구조체 재활용 (ID `int` / Username `string`)
- YAML 키: `reviewers`

### 2.2 loadConfig 환경변수 파싱 — `config.go:49-119`

`AUTHORS` 파싱 블록(`config.go:103-108`) 바로 아래에 `REVIEWERS` 블록 추가:

```go
// config.go:108 이후에 삽입
if env := env.Getenv("REVIEWERS"); env != "" {
    config.Reviewers, err = parseAuthors(env)
    if err != nil {
        return nil, fmt.Errorf("error parsing REVIEWERS environment variable: %v", err)
    }
}
```

- `parseAuthors()` 함수 그대로 재활용 (쉼표 구분, 숫자면 ID, 문자열이면 Username)
- 기존 함수 수정 없음

### 2.3 필터 함수 추가 — `main.go` (신규)

```go
func filterMergeRequestsByReviewer(mrs []*MergeRequestWithApprovals, reviewers []ConfigAuthor) []*MergeRequestWithApprovals {
    if len(reviewers) == 0 {
        return mrs
    }

    var filteredMRs []*MergeRequestWithApprovals
    for _, mr := range mrs {
        for _, reviewer := range reviewers {
            if matchesReviewer(mr.MergeRequest.Reviewers, reviewer) {
                filteredMRs = append(filteredMRs, mr)
                break
            }
        }
    }
    return filteredMRs
}

func matchesReviewer(reviewers []*gitlab.BasicUser, target ConfigAuthor) bool {
    for _, r := range reviewers {
        if (target.ID != 0 && target.ID == r.ID) ||
            (target.Username != "" && target.Username == r.Username) {
            return true
        }
    }
    return false
}
```

**설계 결정:**
- `matchesReviewer`를 별도 헬퍼로 분리 — 이중 루프(MR × reviewer × configAuthor)의 가독성 확보
- `filterMergeRequestsByAuthor`와 동일한 시그니처 패턴 유지
- `reviewers` 비어있으면 전체 통과 (기존 authors와 동일)

### 2.4 execute() 흐름 변경 — `main.go:97-128`

**현재 (`main.go:111`):**
```go
mrs = filterMergeRequestsByAuthor(mrs, config.Authors)
```

**변경 후:**
```go
mrs = filterMergeRequestsByAuthor(mrs, config.Authors)
mrs = filterMergeRequestsByReviewer(mrs, config.Reviewers)
```

**필터 조합 동작:**

| authors | reviewers | 동작 |
|---------|-----------|------|
| 비어있음 | 비어있음 | 전체 MR 통과 (기존 동작) |
| 설정됨 | 비어있음 | 작성자 기준만 필터 |
| 비어있음 | 설정됨 | 리뷰어 기준만 필터 |
| 설정됨 | 설정됨 | 작성자 AND 리뷰어 교집합 |

두 필터를 순차 적용(파이프라인)하므로 자연스럽게 AND 조합이 된다.

### 2.5 Slack 메시지 포매팅 변경 — `main.go:130-158`

`formatMergeRequestsSummary` 함수에 리뷰어 정보 행 추가:

**현재 출력:**
```
:arrow_forward: <URL|Title>
*Author:* John Doe
*Created at:* 12 April 2026, 10:00 UTC
*Approved by:* Jane Doe
```

**변경 후 출력:**
```
:arrow_forward: <URL|Title>
*Author:* John Doe
*Reviewers:* Jane Doe, Bob Smith
*Created at:* 12 April 2026, 10:00 UTC
*Approved by:* Jane Doe
```

**코드 변경:**

`main.go:145-148`의 `Sprintf` 포맷 문자열을 수정:

```go
// 리뷰어 이름 목록 추출
reviewerNames := make([]string, len(mr.MergeRequest.Reviewers))
for i, r := range mr.MergeRequest.Reviewers {
    reviewerNames[i] = r.Name
}
reviewers := strings.Join(reviewerNames, ", ")
if reviewers == "" {
    reviewers = "None"
}

summary += fmt.Sprintf(
    ":arrow_forward: <%s|%s>\n*Author:* %s\n*Reviewers:* %s\n*Created at:* %s\n*Approved by:* %s\n",
    mr.MergeRequest.WebURL, mr.MergeRequest.Title,
    mr.MergeRequest.Author.Name, reviewers, createdAtStr, approvedBy,
)
```

---

## 3. 테스트 설계

### 3.1 config_test.go — 설정 로드 테스트

| 테스트 케이스 | 입력 | 기대 결과 |
|--------------|------|-----------|
| REVIEWERS 환경변수 파싱 | `REVIEWERS=1,username,123` | `[]ConfigAuthor{{ID:1},{Username:"username"},{ID:123}}` |
| config.test.yaml에서 reviewers 로드 | YAML에 reviewers 항목 | 정상 파싱 |
| REVIEWERS 미설정 | 환경변수 없음 | `config.Reviewers` == nil (빈 슬라이스) |

### 3.2 main_test.go — 필터 함수 테스트

| 테스트 케이스 | 설명 |
|--------------|------|
| `TestFilterMergeRequestsByReviewer` | 리뷰어 ID/Username 매칭 — 2개 MR 중 1개 통과 |
| `TestFilterMergeRequestsByReviewer_NoMatch` | 매칭되는 리뷰어 없음 — 0개 반환 |
| `TestFilterMergeRequestsByReviewer_MultipleReviewers` | MR에 여러 리뷰어 중 하나 매칭 — 통과 |
| `TestFilterMergeRequestsByReviewer_EmptyReviewers` | reviewers 설정 비어있음 — 전체 통과 |
| `TestFilterMergeRequestsByReviewer_NoReviewersOnMR` | MR에 리뷰어 미배정 — 필터에 걸림 |

**테스트 데이터 구조 예시:**
```go
mrs := []*MergeRequestWithApprovals{
    {
        MergeRequest: &gitlab.MergeRequest{
            Author: &gitlab.BasicUser{ID: 1, Name: "Alice", Username: "alice"},
            Reviewers: []*gitlab.BasicUser{
                {ID: 10, Name: "Bob", Username: "bob"},
                {ID: 11, Name: "Carol", Username: "carol"},
            },
        },
    },
}
reviewers := []ConfigAuthor{{Username: "bob"}}
```

---

## 4. 설정 파일 변경

### 4.1 config.yaml.example

```yaml
gitlab:
  url: https://gitlab.com
  token: your-gitlab-token
slack:
  webhook_url: https://hooks.slack.com/services/your-slack-webhook-url
projects:
  - id: 123
  - id: 456
groups:
  - id: 1
  - id: 2
cron_schedule: "0 7,13 * * 1-5"
authors:
  - username: "janedoe"
  - username: "johndoe"
  - id: 918
reviewers:                          # 추가
  - username: "reviewer1"
  - id: 100
```

### 4.2 config.test.yaml

기존 내용 끝에 `reviewers` 항목 추가:
```yaml
reviewers:
  - username: "reviewer1"
  - id: 200
```

---

## 5. 구현 순서

| 순서 | 파일 | 작업 | 의존성 |
|------|------|------|--------|
| 1 | `config.go` | `Config.Reviewers` 필드 추가 | 없음 |
| 2 | `config.go` | `loadConfig()`에 `REVIEWERS` 환경변수 파싱 추가 | 1 |
| 3 | `config.test.yaml` | `reviewers` 테스트 데이터 추가 | 없음 |
| 4 | `config_test.go` | Reviewers 환경변수/파일 로드 테스트 | 1, 2, 3 |
| 5 | `main.go` | `filterMergeRequestsByReviewer()` + `matchesReviewer()` | 1 |
| 6 | `main.go` | `execute()`에 리뷰어 필터 삽입 | 5 |
| 7 | `main.go` | `formatMergeRequestsSummary()` 리뷰어 표시 | 없음 |
| 8 | `main_test.go` | 리뷰어 필터 테스트 5건 | 5 |
| 9 | `config.yaml.example` | reviewers 예시 추가 | 없음 |

---

## 6. 하위 호환성

| 시나리오 | 동작 |
|----------|------|
| `reviewers` 미설정 (YAML에 없음) | `Config.Reviewers` == nil → 필터 스킵 → 기존과 동일 |
| `REVIEWERS` 환경변수 미설정 | 위와 동일 |
| `authors`만 사용하는 기존 사용자 | 영향 없음 |

YAML에 `reviewers` 키가 없어도 Go의 `yaml.Unmarshal`은 에러 없이 nil로 둔다.
환경변수도 미설정이면 파싱 블록 자체를 건너뛴다.
**기존 설정 파일 수정 없이 그대로 동작한다.**

---

## 7. 대안 검토

### 7.1 통합 필터 함수 vs 별도 함수

| 방안 | 장점 | 단점 |
|------|------|------|
| **A: 별도 함수** (채택) | 기존 코드 수정 최소화, 단일 책임, 테스트 독립적 | 함수가 하나 더 늘어남 |
| B: 기존 함수 확장 | 함수 수 유지 | 시그니처 변경, 기존 테스트 깨짐, 복잡도 증가 |

**결정: 별도 함수 (A안)**  
기존 `filterMergeRequestsByAuthor`는 그대로 두고 `filterMergeRequestsByReviewer`를 추가한다.
기존 테스트에 영향 없고, 각 필터를 독립적으로 테스트할 수 있다.

### 7.2 OR 조합 지원 여부

Plan에서 OR 조합은 범위 외로 명시했다. 현재 파이프라인(순차 필터)은 자연스럽게 AND 동작이다.
향후 OR이 필요하면 `filterMergeRequests(mrs, config)` 통합 함수로 리팩토링 가능하나,
YAGNI 원칙에 따라 현 단계에서는 구현하지 않는다.
