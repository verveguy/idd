#!/usr/bin/env bash
#
# build.sh — build the Claude Desktop / claude.ai Skill ZIP from the master.
#
# Single source of truth: plugins/idd/ids-data/  (validate.py + *.json + NOTICE.md)
# This script syncs that into the Desktop skill bundle, (optionally) stamps a new
# version across every manifest, builds dist/idd-character-builder.zip, and
# smoke-tests the packaged validator.
#
# Usage:
#   ./build.sh                 # sync + build + smoke-test with current version
#   ./build.sh --version 1.0.3 # also stamp 1.0.3 into plugin/marketplace/skill
#
set -euo pipefail

# Always run from the repo root (the dir containing this script).
cd "$(dirname "$0")"

SRC="plugins/idd/ids-data"
BUNDLE="dist/idd-character-builder"
SCRIPTS="$BUNDLE/scripts"
ZIP="dist/idd-character-builder.zip"
PLUGIN_MANIFEST="plugins/idd/.claude-plugin/plugin.json"
MARKET_MANIFEST=".claude-plugin/marketplace.json"
SKILL_MD="$BUNDLE/SKILL.md"

NEW_VERSION=""
if [[ "${1:-}" == "--version" ]]; then
  NEW_VERSION="${2:-}"
  [[ -n "$NEW_VERSION" ]] || { echo "error: --version needs a value (e.g. 1.0.3)"; exit 1; }
  [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "error: version must be X.Y.Z"; exit 1; }
fi

echo "==> master:  $SRC"
echo "==> bundle:  $BUNDLE"

# 1. Sync validator + data + notice from the master into the skill bundle.
mkdir -p "$SCRIPTS"
# Remove stale bundled data so deletions in master propagate, then copy fresh.
rm -f "$SCRIPTS"/*.json "$SCRIPTS"/validate.py "$SCRIPTS"/NOTICE.md
cp "$SRC"/*.json "$SRC"/validate.py "$SRC"/NOTICE.md "$SCRIPTS"/
echo "==> synced $(ls "$SCRIPTS"/*.json | wc -l | tr -d ' ') data files + validate.py + NOTICE.md"

# 2. Optionally stamp a new version across all manifests + the skill.
if [[ -n "$NEW_VERSION" ]]; then
  echo "==> stamping version $NEW_VERSION"
  perl -i -pe 's/"version":\s*"[0-9]+\.[0-9]+\.[0-9]+"/"version": "'"$NEW_VERSION"'"/' \
    "$PLUGIN_MANIFEST" "$MARKET_MANIFEST"
  perl -i -pe 's/^(\s*version:\s*)"[0-9]+\.[0-9]+\.[0-9]+"/${1}"'"$NEW_VERSION"'"/' "$SKILL_MD"
fi

# 3. Verify the master validator and the bundled copy are byte-identical.
diff -q "$SRC/validate.py" "$SCRIPTS/validate.py" >/dev/null \
  && echo "==> validator in sync (master == bundle)" \
  || { echo "error: validator copies differ"; exit 1; }

# 4. Build the ZIP (top-level folder must match the skill name).
rm -f "$ZIP"
( cd dist && zip -rq "$(basename "$ZIP")" idd-character-builder -x '*.DS_Store' )
echo "==> built $ZIP ($(du -h "$ZIP" | cut -f1 | tr -d ' '))"

# 5. Smoke-test the PACKAGED skill exactly as Claude's sandbox runs it:
#    fresh-extract the zip, cd into the skill root, run with relative paths.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
( cd "$TMP" && unzip -oq "$OLDPWD/$ZIP" )
SKROOT="$TMP/idd-character-builder"

# 5a. catalog must load
python3 "$SKROOT/scripts/validate.py" --catalog >/dev/null \
  && echo "==> smoke: catalog loads OK"

# 5b. a minimal legal build must PASS (build file lives in the skill root)
cat > "$SKROOT/ok.json" <<'JSON'
{"name":"Smoke OK","heritage":"Solari","faction":"The Veilward Enclave",
 "cp_sources":{"starting":30},
 "attributes":{"Air":2,"Earth":2,"Fire":2,"Water":2,"Void":2},
 "headers":[],"skills":[{"name":"Celerity"}]}
JSON
if ( cd "$SKROOT" && python3 scripts/validate.py ok.json >/dev/null ); then
  echo "==> smoke: legal build PASSES OK"
else
  echo "error: legal build failed to validate"; exit 1
fi

# 5c. an illegal build (no faction) must FAIL (exit 2)
cat > "$SKROOT/bad.json" <<'JSON'
{"name":"Smoke Bad","heritage":"Solari",
 "cp_sources":{"starting":30},
 "attributes":{"Air":2,"Earth":2,"Fire":2,"Water":2,"Void":2},
 "headers":[],"skills":[]}
JSON
if ( cd "$SKROOT" && python3 scripts/validate.py bad.json >/dev/null ); then
  echo "error: factionless build was accepted — hard-block regression!"; exit 1
else
  echo "==> smoke: illegal build correctly REJECTED"
fi

# 6. Summary.
echo
echo "BUILD OK"
echo "  version(s):"
echo "    plugin.json      $(perl -ne 'print $1 if /"version":\s*"([0-9.]+)"/' "$PLUGIN_MANIFEST")"
echo "    marketplace.json $(perl -ne 'print "$1 " if /"version":\s*"([0-9.]+)"/' "$MARKET_MANIFEST")"
echo "    SKILL.md         $(perl -ne 'print $1 if /^\s*version:\s*"([0-9.]+)"/' "$SKILL_MD")"
echo "  artifact: $ZIP"
echo
echo "Next: git add -A && git commit && git push, then:"
echo "  gh release create v<VERSION> --notes '...' $ZIP"
