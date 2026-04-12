# mr-noti-bot — Claude Code 프로젝트 규칙
> 기반: ai-dev-toolkit v0.3.0.0
>
> 위치: `<프로젝트 루트>/CLAUDE.md`
> 글로벌 규칙(`~/.claude/CLAUDE.md`)을 상속하며, 이 파일에서 오버라이드/추가합니다.
>
> placeholder는 `/project-configure`로 구체화하세요.

---

## 프로젝트 개요

```
프로젝트명  : mr-noti-bot
설명        : GitLab MR → Slack 알림 봇 (Go)
주요 언어   : Go
```

---

## 1. 프로젝트 디렉토리 구조

```
mr-noti-bot/
├── CLAUDE.md
├── main.go                ← 진입점
├── config.go              ← 설정 로드
├── gitlab.go              ← GitLab API 클라이언트
├── slack.go               ← Slack 웹훅 전송
├── mocks/                 ← 테스트 모의 객체
├── Dockerfile
├── k8s/                   ← Kubernetes 매니페스트
├── cdk/                   ← AWS CDK 배포
├── config.yaml.example    ← 설정 예시 (시크릿 미포함)
└── docs/
```

---

## 2. 환경별 설정

### WSL2
```bash
# Go 개발 환경 — WSL2 내부에서 빌드/테스트
go test ./...
go run .
```

---

## 3. 아키텍처 규칙

```
cron/시작
  → GitLab API (MR 목록 조회)
    → 필터링 (작성자, 상태)
      → Slack 웹훅 전송
```

---

## 4. 프로젝트별 코드 규칙

### 4.1 네이밍 컨벤션
- Go 표준: `camelCase` (비공개), `PascalCase` (공개)

### 4.2 설정/시크릿 파일 규칙
- `config.yaml`에 실제 토큰 포함 — **절대 커밋 금지**
- 새 설정 항목 추가 시 `config.yaml.example`도 함께 갱신

---

## 5. 빌드/실행 명령어

```bash
go build -o mr-noti-bot .    # 빌드
go test ./...                 # 테스트
go run .                      # 실행
docker build -t mr-noti-bot . # Docker 빌드
```

---

## 6. Git 워크플로우

### 브랜치 구조
```
main              ← 안정 브랜치
feature/<기능명>   ← 기능 개발
```

---

## 7. 산출물 및 지침 디렉토리

```
docs/
├── artifacts/           ← 작업 단위 산출물 (번호.작업명.md)
│   ├── ideation/
│   ├── design/
│   ├── review/
│   ├── summary/
│   └── test-report/
└── stack/               ← 기술 스택별 지침
```

---

## 8. 알려진 이슈 및 해결책

| 환경 | 증상 | 해결책 |
|------|------|--------|
| | | |

---

*글로벌 규칙과 충돌 시 이 파일의 규칙이 우선합니다.*
