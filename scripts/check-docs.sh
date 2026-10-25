#!/bin/bash
# Check that changed source files have an L2 module README.
# L3 file headers (input/output/pos/note) are retired.

set -e

# Get changed files (staged or last commit)
CHANGED_FILES=$(git diff --cached --name-only --diff-filter=ACMR 2>/dev/null || git diff HEAD~1 --name-only --diff-filter=ACMR)

if [ -z "$CHANGED_FILES" ]; then
    echo "No changed files to check"
    exit 0
fi

WARNINGS=0

for FILE in $CHANGED_FILES; do
    # Skip non-source files
    case "$FILE" in
        *.go|*.ts|*.js|*.tsx|*.jsx|*.py|*.java|*.rs|*.rb|*.php) ;;
        *) continue ;;
    esac

    # Skip if file doesn't exist (deleted)
    [ -f "$FILE" ] || continue

    # Check if README.md exists for the directory
    DIR=$(dirname "$FILE")
    if [ ! -f "$DIR/README.md" ] && [ "$DIR" != "." ] && [ "$DIR" != "cmd" ]; then
        echo "WARN: $DIR/README.md not found (L2 missing)"
        WARNINGS=$((WARNINGS + 1))
    fi
done

echo ""
echo "================================"
echo "Documentation Check Summary"
echo "================================"
echo "Files checked: $(echo "$CHANGED_FILES" | grep -E '\.(go|ts|js|tsx|jsx|py|java|rs|rb|php)$' | wc -l | tr -d ' ')"
echo "Warnings: $WARNINGS"
echo "PASSED"
exit 0
