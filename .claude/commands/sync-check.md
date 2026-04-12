blueprints/와 배포 파일(.claude/rules/, claude/global_CLAUDE.md) 간 동기화 상태를 점검한다.

## 수행 절차

1. **rules 동기화 확인**
   - `.claude/rules/` 각 파일의 "원본" 주석에서 blueprint 출처를 추출
   - 해당 blueprint 파일과 내용이 일치하는지 비교
   - 불일치 항목을 표로 정리

2. **글로벌 CLAUDE.md 확인**
   - `claude/global_CLAUDE.md`가 `~/.claude/CLAUDE.md` 심볼릭 링크 타겟인지 확인
   - 링크가 끊어졌거나 다른 파일을 가리키면 경고

3. **줄 수 점검**
   - 지침 파일(SKILL.md, blueprints/*.md, CLAUDE.md, rules/*.md)의 줄 수 확인
   - 200줄 초과 파일을 경고로 표시

4. **결과 출력**
   - 동기화 상태를 표 형식으로 정리
   - 문제 발견 시 수정 제안 포함

## 규칙

- 파일을 수정하지 않는다 — 점검 + 보고만 수행
- 불일치가 있으면 어느 쪽이 최신인지 git log로 판단하여 방향 제안
