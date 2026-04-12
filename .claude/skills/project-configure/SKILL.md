---
name: project-configure
description: 대화형으로 프로젝트의 AI 도구 설정을 구체화 — 기술 스택, 모듈 구조, 컨벤션 등을 채워넣음
argument-hint: "[target-directory-path]"
allowed-tools: Read, Bash, Glob, Grep, Write, Edit
---

# Project Configure

`/project-init`으로 복제된 템플릿을 사용자와 대화하며 프로젝트에 맞게 구체화한다.

## 인자

- `$0` — 타겟 디렉토리 경로 (선택, 미지정 시 현재 디렉토리)

## 매뉴얼 저장소(blueprints) 참조

```
# 원본 (ai-dev-toolkit 내부에서 실행 시) — project-init이 복제할 때 절대 경로로 치환됨
REPO_DIR=${CLAUDE_SKILL_DIR}/../../..
```

이 경로의 `blueprints/` 디렉토리에서 참고 자료를 검색하여 프로젝트에 적합한 내용을 제안한다.
경로가 유효하지 않으면 사용자에게 매뉴얼 저장소 위치를 확인한다.

## 수집할 정보

프로젝트에 대해 아래 정보를 대화형으로 수집한다:

### 1. 프로젝트 기본 정보
- 프로젝트명
- 프로젝트 설명/목적
- 주요 언어/프레임워크 (C, C++, Python, Rust 등)

### 2. 임베디드 프로젝트인 경우
- 타겟 보드/MCU
- 툴체인
- OS/RTOS
- 빌드 시스템 (Make, CMake 등)

### 3. 프로젝트 구조
- 디렉토리 레이아웃 (기존 구조 감지 또는 새로 설계)
- 레이어 아키텍처 (있는 경우)
- 모듈 접두사 네이밍

### 4. 워크플로우
- 브랜치 전략
- 빌드/테스트 명령어
- Git hooks 필요 여부

### 5. 개발 워크플로우
- 작업 유형별 프리셋 분류 기준 커스터마이즈 (기본: blueprints/design-principles.md의 기준)
  - 예: "DB 스키마 변경은 항상 Large" 등 프로젝트 고유 규칙
- 테스트 전략 (단위/통합/E2E)

### 6. AI 도구 추가 설정
- 프로젝트 레벨 스킬 필요 여부
- 특별한 코드 규칙이나 제약사항

## 실행 흐름

1. **현재 상태 파악** — 타겟 디렉토리의 `CLAUDE.md`, `.claude/` 존재 확인
   - init이 안 되어 있으면 → 먼저 `/project-init` 실행을 안내
2. **기존 프로젝트 분석** — 타겟에 이미 코드가 있으면 구조를 분석하여 제안에 활용
3. **1문 1답 대화형 수집** — 아래 규칙을 따른다
4. **구성 플랜 작성** — 수집된 정보로 구성 계획서를 작성하여 사용자 검토
5. **적용** — 승인 후 `CLAUDE.md` 및 관련 파일의 placeholder를 실제 값으로 채움
6. **결과 요약** — 변경된 파일 목록과 프로젝트 설정 요약 출력

### 대화형 수집 규칙

**한 번에 질문 1개.** 답변을 받은 뒤 다음 질문으로 넘어간다.

- 각 질문에 **추천값 또는 기본값**을 함께 제시한다
- 기존 코드/파일에서 자동 감지 가능한 항목은 감지 결과를 보여주고 **확인만 요청**한다
- 이전 답변에 따라 **불필요한 질문은 건너뛴다** (예: 임베디드가 아니면 섹션 2 스킵)
- 각 질문 앞에 진행 표시를 붙인다: `[1/N]`, `[2/N]`, ...
  - N은 이전 답변에 따라 동적으로 조정된다
- 관련 맥락이 있는 경우 2개까지 묶을 수 있으나, **3개 이상은 금지**

## blueprints 마이그레이션 및 rules 배포

대화형 수집 과정에서 프로젝트 기술 스택이 파악되면, 관련 blueprint를 읽고 **대화를 통해 프로젝트에 맞게 마이그레이션**한다.

### 관련 blueprint 및 rules 판별

| 조건 | blueprint | rules 템플릿 |
|------|-----------|-------------|
| C/C++ 프로젝트 | `blueprints/coding-standards.md` | `.claude/rules/project-coding-standards.md` |
| 임베디드 프로젝트 | `blueprints/build-environment.md` | `.claude/rules/project-build-environment.md` |
| Git 사용 (거의 항상) | `blueprints/git-workflow.md` | `.claude/rules/project-git-workflow.md` |
| 모든 프로젝트 | `blueprints/design-principles.md` | (글로벌 rules로 제공) |

### rules 배포 절차

기술 스택이 확정되면:
1. `<target>/.claude/rules/` 디렉토리 생성 (`mkdir -p`)
2. 조건에 맞는 rules 템플릿을 복제 (`project-` 접두사 제거)
   - 예: `project-coding-standards.md` → `<target>/.claude/rules/coding-standards.md`
3. 복제된 rules 파일 내 프로젝트 커스터마이즈 (모듈 접두사, 브랜치 전략 등 대화에서 수집한 정보 반영)

### rules와 docs/stack/ 역할 분리

- `.claude/rules/*.md` — Claude Code가 자동 로드하는 **짧은 행동 규칙** (80줄 이내 권장)
- `docs/stack/*.md` — blueprint를 프로젝트에 마이그레이션한 **상세 참조 문서** (줄 수 제한 없음)

### 마이그레이션 흐름

대화 중 기술 스택이 확정되면:
1. 관련 blueprint를 읽고 프로젝트 맥락에 맞춰 조정이 필요한 부분을 파악
2. 대화 질문에 blueprint 내용을 자연스럽게 반영 — 예: "blueprint에 MISRA 규칙이 있는데, 이 프로젝트에도 적용할까요?"
3. 사용자 답변을 반영하여:
   a. `.claude/rules/<주제>.md`에 핵심 규칙 (Claude Code 자동 로드용)
   b. `docs/stack/<주제>.md`에 프로젝트 버전 상세 문서 (참조용)

```markdown
<!-- docs/stack/ 파일 헤더 -->
# C/C++ 코딩 표준
> 원본: ai-dev-toolkit blueprints/coding-standards.md (v0.1.0)
> 마이그레이션: 프로젝트 고유 네이밍 규칙 추가, MISRA 섹션 제거
```

## 주의사항

- **1문 1답 원칙** — 위 "대화형 수집 규칙" 참조
- 기본값/추천값을 항상 제시하여 사용자가 빠르게 결정할 수 있도록 한다
- 구성 플랜을 반드시 사용자에게 보여주고 승인 후 적용한다
- 적용 후 자동 커밋하지 않음 — 사용자가 검토 후 커밋
