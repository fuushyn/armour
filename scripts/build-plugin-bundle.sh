#!/bin/bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${ROOT_DIR}/dist"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
ASSET_NAME="armour-plugin-${GOOS}-${GOARCH}.tar.gz"

WORK_DIR="$(mktemp -d)"
PLUGIN_DIR="${WORK_DIR}/armour-plugin"

mkdir -p "${PLUGIN_DIR}/.claude-plugin"
mkdir -p "${OUT_DIR}"

echo "Building armour binary for ${GOOS}/${GOARCH}..."
(cd "${ROOT_DIR}" && GOOS="${GOOS}" GOARCH="${GOARCH}" go build -o "${PLUGIN_DIR}/armour" ./)

cp "${ROOT_DIR}/.claude-plugin/plugin.json" "${PLUGIN_DIR}/.claude-plugin/plugin.json"
cp "${ROOT_DIR}/.claude-plugin/marketplace.json" "${PLUGIN_DIR}/.claude-plugin/marketplace.json"
cp -R "${ROOT_DIR}/commands" "${PLUGIN_DIR}/commands"
cp -R "${ROOT_DIR}/scripts" "${PLUGIN_DIR}/scripts"
cp -R "${ROOT_DIR}/hooks" "${PLUGIN_DIR}/hooks"
cp "${ROOT_DIR}/README.md" "${PLUGIN_DIR}/README.md"
if [ -f "${ROOT_DIR}/LICENSE" ]; then
  cp "${ROOT_DIR}/LICENSE" "${PLUGIN_DIR}/LICENSE"
fi

tar -czf "${OUT_DIR}/${ASSET_NAME}" -C "${PLUGIN_DIR}" .

echo "Bundle created: ${OUT_DIR}/${ASSET_NAME}"
