#!/bin/bash
# PostToolUse Hook: 지침 파일 수정 후 줄 수 검사
# 200줄 초과 시 경고 출력 (non-blocking — exit 0)

FILE="$1"
[ -z "$FILE" ] && exit 0
[ ! -f "$FILE" ] && exit 0

# 지침 파일만 검사 (SKILL.md, blueprints/, CLAUDE.md, rules/)
case "$FILE" in
  */SKILL.md|*/blueprints/*.md|*/CLAUDE.md|*/.claude/rules/*.md)
    LINES=$(wc -l < "$FILE")
    if [ "$LINES" -gt 200 ]; then
      echo "[warn] $FILE: ${LINES}줄 — 200줄 제한 초과. /optimize-docs 실행 권장"
    fi
    ;;
esac

exit 0
