아키텍처 결정 기록(ADR)을 생성하거나 조회한다.

## 사용법

- `/decision` — 기존 ADR 목록 조회
- `/decision $ARGUMENTS` — 새 ADR 생성 (제목을 인자로)

## 새 ADR 생성 절차

1. `docs/decisions/` 디렉토리의 기존 ADR 번호를 확인하여 다음 번호 결정
2. `.claude/templates/decision.template.md`를 읽고 구조를 따른다
3. `docs/decisions/ADR-<번호>-<제목-kebab>.md` 파일을 생성
4. 대화 맥락에서 결정 배경, 선택지, 이유를 채운다
5. 상태는 "채택"으로 설정

## ADR 목록 조회 절차

1. `docs/decisions/` 내 모든 ADR 파일을 읽는다
2. 번호, 제목, 상태, 날짜를 표로 정리

## 규칙

- 번호는 순차 증가 (001, 002, ...)
- 기존 ADR을 수정하지 않는다 — 대체 시 새 ADR 작성 후 기존 상태를 "대체됨"으로 변경
- 사소한 결정은 기록하지 않는다 — 아키텍처, 도구 선택, 구조 변경 등 중요한 결정만
