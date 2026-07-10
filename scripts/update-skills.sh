#!/usr/bin/env bash
# Update the .claude/skills submodule to a pinned tag and record a state-log event.
# Usage: ./scripts/update-skills.sh [tag]   (default: latest tag)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
SM=".claude/skills"

if ! git config --file .gitmodules --get "submodule.${SM}.url" >/dev/null 2>&1; then
  echo "No submodule configured at ${SM} — check .gitmodules." >&2
  exit 1
fi
if git config --file .gitmodules --get "submodule.${SM}.url" | grep -q PLACEHOLDER; then
  echo "Set the real skills-repo URL in .gitmodules first." >&2
  exit 1
fi

git submodule update --init "$SM"
git -C "$SM" fetch --tags --quiet

TAG="${1:-$(git -C "$SM" describe --tags "$(git -C "$SM" rev-list --tags --max-count=1)")}"
CURRENT="$(git -C "$SM" describe --tags --always)"

if [[ "$TAG" == "$CURRENT" ]]; then
  echo "skills already at ${TAG}"
  exit 0
fi

git -C "$SM" checkout --quiet "$TAG"

TS="$(date -u +%Y-%m-%dT%H%M)"
EVENT="docs/state-log/${TS}-skills-updated.yaml"
cat > "$EVENT" << EVT
event: skills-updated
timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)
actor: $(git config user.name || echo unknown)
summary: skills submodule updated ${CURRENT} -> ${TAG}
refs:
  adr: []
  commit: null
  files: [.claude/skills]
EVT

git add "$SM" "$EVENT"
echo "skills updated ${CURRENT} -> ${TAG}."
echo "Review the skills repo changelog for contract changes, then commit:"
echo "  git commit -m 'chore(skills): update to ${TAG}'"
