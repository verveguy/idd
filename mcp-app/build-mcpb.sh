#!/usr/bin/env bash
#
# build-mcpb.sh — cross-compile the IDD MCP App server and package it as a
# one-click Claude Desktop extension (.mcpb). The builder UI + ruleset are
# embedded in the binary (go:embed), so the bundle is just the binaries + manifest.
#
set -euo pipefail
cd "$(dirname "$0")"

OUT="dist"
STAGE="$OUT/mcpb-stage"
MCPB="$OUT/in-darkened-dreams.mcpb"

rm -rf "$STAGE" "$MCPB"
mkdir -p "$STAGE/server"

echo "==> cross-compiling (CGO disabled, static)…"
export CGO_ENABLED=0
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$OUT/darwin-arm64" .
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$OUT/darwin-amd64" .
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$STAGE/server/idd-mcp-linux" .
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$STAGE/server/idd-mcp-win.exe" .

# Universal macOS binary (arm64 + Intel) so one bundle covers every Mac.
if command -v lipo >/dev/null 2>&1; then
  lipo -create -output "$STAGE/server/idd-mcp-darwin" "$OUT/darwin-arm64" "$OUT/darwin-amd64"
  echo "==> macOS universal binary built"
else
  cp "$OUT/darwin-arm64" "$STAGE/server/idd-mcp-darwin"
  echo "==> lipo unavailable — shipping arm64-only macOS binary"
fi
chmod +x "$STAGE/server/idd-mcp-darwin" "$STAGE/server/idd-mcp-linux"
rm -f "$OUT/darwin-arm64" "$OUT/darwin-amd64"

cp mcpb/manifest.json "$STAGE/manifest.json"

echo "==> packaging $MCPB"
( cd "$STAGE" && zip -rq "../$(basename "$MCPB")" manifest.json server -x '*.DS_Store' )
rm -rf "$STAGE"

echo
echo "BUILD OK — $MCPB"
unzip -l "$MCPB" | awk 'NR>3 && $4 {printf "  %-10s %s\n", $1, $4}' | grep -v '^\s*-' || true
echo "  size: $(du -h "$MCPB" | cut -f1 | tr -d ' ')"
