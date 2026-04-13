#!/bin/bash
# Check if changed files have L3 headers and affected L2 docs are updated

set -e

# Get changed files (staged or last commit)
CHANGED_FILES=$(git diff --cached --name-only --diff-filter=ACMR 2>/dev/null || git diff HEAD~1 --name-only --diff-filter=ACMR)

if [ -z "$CHANGED_FILES" ]; then
    echo "No changed files to check"
    exit 0
fi

ERRORS=0
WARNINGS=0

for FILE in $CHANGED_FILES; do
    # Skip non-source files
    case "$FILE" in
        *.go|*.ts|*.js|*.tsx|*.jsx|*.py|*.java|*.rs|*.rb|*.php) ;;
        *) continue ;;
    esac

    # Skip if file doesn't exist (deleted)
    [ -f "$FILE" ] || continue

    # Check L3 header
    if ! head -6 "$FILE" | grep -q "input:"; then
        echo "ERROR: $FILE missing L3 header (input:)"
        ERRORS=$((ERRORS + 1))
    fi
    if ! head -6 "$FILE" | grep -q "output:"; then
        echo "ERROR: $FILE missing L3 header (output:)"
        ERRORS=$((ERRORS + 1))
    fi
    if ! head -6 "$FILE" | grep -q "pos:"; then
        echo "ERROR: $FILE missing L3 header (pos:)"
        ERRORS=$((ERRORS + 1))
    fi

    # For Go files, check Package comment
    if [[ "$FILE" == *.go ]]; then
        if ! head -6 "$FILE" | grep -q "// Package "; then
            echo "ERROR: $FILE missing Package comment (// Package <name> ...)"
            ERRORS=$((ERRORS + 1))
        fi
    fi

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
echo "Errors: $ERRORS"
echo "Warnings: $WARNINGS"

if [ $ERRORS -gt 0 ]; then
    echo ""
    echo "FAILED: Fix L3 headers before commit"
    echo "Format (Go):"
    echo "  // Package <name> provides/does ..."
    echo "  // input: what this file depends on"
    echo "  // output: what this file exports"
    echo "  // pos: this file's role in the system"
    echo "  // note: if changed, update this header and README.md"
    echo "Format (other languages):"
    echo "  // input: what this file depends on"
    echo "  // output: what this file exports"
    echo "  // pos: this file's role in the system"
    echo "  // note: if changed, update this header and README.md"
    exit 1
fi

echo "PASSED"
exit 0
