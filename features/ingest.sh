#!/usr/bin/env bash
set -euo pipefail

FEATURES_DIR="$(cd "$(dirname "$0")" && pwd)"
ACTIVE_DIR="$FEATURES_DIR/active"
BACKLOG="$FEATURES_DIR/BACKLOG.md"
TEMPLATE="$FEATURES_DIR/templates/FEATURE.md"

usage() {
  echo "Usage: $0 [issue-number]"
  echo ""
  echo "  No arguments: list open GitHub issues not yet ingested"
  echo "  issue-number: ingest that issue into the feature pipeline"
}

# Get list of already-ingested issue numbers from active/ and completed/ filenames
ingested_issues() {
  for f in "$ACTIVE_DIR"/*.md "$FEATURES_DIR/completed"/*.md; do
    [ -f "$f" ] || continue
    basename "$f" | grep -oE '^[0-9]+' || true
  done | sort -u
}

# List open issues not yet ingested
list_uningest() {
  echo "Open GitHub issues not yet in the pipeline:"
  echo ""

  local ingested
  ingested=$(ingested_issues)

  gh issue list --state open --json number,title --jq '.[] | "\(.number)\t\(.title)"' | while IFS=$'\t' read -r num title; do
    if ! echo "$ingested" | grep -qx "$num"; then
      printf "  #%-4s %s\n" "$num" "$title"
    fi
  done
}

# Slugify a title: lowercase, replace non-alphanum with hyphens, trim
slugify() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g; s/--*/-/g; s/^-//; s/-$//' | cut -c1-50
}

# Ingest a single issue
ingest_issue() {
  local issue_num="$1"

  # Check if already ingested
  if ls "$ACTIVE_DIR"/"${issue_num}"-*.md 1>/dev/null 2>&1 || \
     ls "$FEATURES_DIR/completed"/"${issue_num}"-*.md 1>/dev/null 2>&1; then
    echo "Error: Issue #${issue_num} is already ingested."
    exit 1
  fi

  # Fetch issue data
  local issue_json
  issue_json=$(gh issue view "$issue_num" --json number,title,body)

  local title body
  title=$(echo "$issue_json" | jq -r '.title')
  body=$(echo "$issue_json" | jq -r '.body // "No description provided."')

  # Generate filename
  local slug
  slug=$(slugify "$title")
  local padded
  padded=$(printf "%03d" "$issue_num")
  local filename="${padded}-${slug}.md"
  local filepath="$ACTIVE_DIR/$filename"

  # Create feature file from template
  sed \
    -e "s|<title>|${title}|g" \
    -e "s|<number>|${issue_num}|g" \
    -e "s|<Summarize the GitHub issue. What problem does this solve?>|${body}|g" \
    "$TEMPLATE" > "$filepath"

  # Append to BACKLOG.md active table
  # Find the blank line after the Active table header and insert before Completed
  local row="| — | #${issue_num} | ${title} | TRIAGE | — |"
  # Insert before the empty line preceding "## Completed"
  sed -i '' "/^## Completed$/i\\
${row}
" "$BACKLOG"

  echo "Ingested issue #${issue_num} as ${filename}"
  echo "  Stage: TRIAGE"
  echo "  File: features/active/${filename}"
  echo "  Backlog updated."
}

# Main
if [ $# -eq 0 ]; then
  list_uningest
elif [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
  usage
else
  ingest_issue "$1"
fi
