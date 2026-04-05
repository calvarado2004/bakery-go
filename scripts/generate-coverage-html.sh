#!/bin/bash
# generate-coverage-html.sh
# Generates HTML coverage report from test coverage output

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Generating Test Coverage HTML Report ==="

# Check which coverage file to use
if [ -f "cover_all.out" ]; then
    COVERAGE_FILE="cover_all.out"
    REPORT_NAME="coverage_all.html"
    echo "Using cover_all.out (full integration test coverage)"
elif [ -f "cover.out" ]; then
    COVERAGE_FILE="cover.out"
    REPORT_NAME="coverage.html"
    echo "Using cover.out (unit test coverage)"
else
    echo "Error: No coverage file found (cover.out or cover_all.out)"
    echo "Run 'go test ./... -coverprofile=cover.out' first"
    exit 1
fi

# Generate HTML report
echo ""
echo "Generating HTML report: $REPORT_NAME"
go tool cover -html="$COVERAGE_FILE" -o "$REPORT_NAME"

# Get coverage summary
echo ""
echo "=== Coverage Summary ==="
go tool cover -func="$COVERAGE_FILE" | grep -E "^total:" || go tool cover -func="$COVERAGE_FILE" | tail -1

# Open in browser (optional - commented out)
# echo ""
# echo "Opening $REPORT_NAME in default browser..."
# open "$REPORT_NAME"  # macOS
# xdg-open "$REPORT_NAME"  # Linux
# sensible-browser "$REPORT_NAME"  # Debian/Ubuntu

echo ""
echo "✓ Coverage report generated: $PROJECT_ROOT/$REPORT_NAME"
echo "  To view: open $REPORT_NAME in your browser"
