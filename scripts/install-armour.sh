#!/bin/bash
set -euo pipefail

REPO="fuushyn/armour"
MARKETPLACE_SOURCE="${ARMOUR_MARKETPLACE_SOURCE:-}"
MARKETPLACE_NAME="armour-marketplace"
PLUGIN_NAME="armour@${MARKETPLACE_NAME}"
SCOPE="user"
SETTINGS_FILE="${CLAUDE_SETTINGS_PATH:-$HOME/.claude/settings.json}"
INSTALL_DIR="${ARMOUR_INSTALL_DIR:-$HOME/.armour/armour-plugin}"

if ! command -v claude >/dev/null 2>&1; then
  echo "Claude Code CLI not found. Install Claude Code first."
  exit 1
fi

marketplace_list() {
  claude plugin marketplace list 2>/dev/null
}

resolve_platform() {
  local uname_s uname_m
  uname_s="$(uname -s)"
  uname_m="$(uname -m)"

  case "${uname_s}" in
    Darwin) goos="darwin" ;;
    Linux) goos="linux" ;;
    *) echo "Unsupported OS: ${uname_s}"; exit 1 ;;
  esac

  case "${uname_m}" in
    x86_64|amd64) goarch="amd64" ;;
    arm64|aarch64) goarch="arm64" ;;
    *) echo "Unsupported architecture: ${uname_m}"; exit 1 ;;
  esac
}

download_bundle() {
  local asset_name download_url tmpfile
  resolve_platform
  asset_name="armour-plugin-${goos}-${goarch}.tar.gz"

  if [ -n "${ARMOUR_RELEASE_URL:-}" ]; then
    download_url="${ARMOUR_RELEASE_URL}"
  elif [ -n "${ARMOUR_RELEASE_TAG:-}" ]; then
    download_url="https://github.com/${REPO}/releases/download/${ARMOUR_RELEASE_TAG}/${asset_name}"
  else
    download_url="https://github.com/${REPO}/releases/latest/download/${asset_name}"
  fi

  tmpfile="$(mktemp -t armour-plugin.XXXXXX.tar.gz)"
  echo "Downloading ${download_url}..."
  curl -fsSL "${download_url}" -o "${tmpfile}"

  if [ -d "${INSTALL_DIR}" ]; then
    rm -rf "${INSTALL_DIR}"
  fi
  mkdir -p "${INSTALL_DIR}"
  tar -xzf "${tmpfile}" -C "${INSTALL_DIR}"
  rm -f "${tmpfile}"

  if [ ! -f "${INSTALL_DIR}/.claude-plugin/marketplace.json" ]; then
    echo "Marketplace manifest not found in ${INSTALL_DIR}."
    exit 1
  fi
}

enable_plugin() {
	set +e
	output="$(claude plugin enable "${PLUGIN_NAME}" --scope "${SCOPE}" 2>&1)"
	status=$?
	set -e

  if [ $status -eq 0 ]; then
    return 0
  fi

  if echo "$output" | grep -qi "not found in disabled plugins"; then
    echo "Plugin already enabled."
    return 0
  fi

  echo "$output"
	return $status
}

ensure_enabled_settings() {
	if ! command -v python3 >/dev/null 2>&1; then
		return 1
	fi

	PLUGIN_NAME_ENV="${PLUGIN_NAME}" SETTINGS_FILE_ENV="${SETTINGS_FILE}" python3 - <<'PY'
import json
import os
from pathlib import Path

plugin_name = os.environ["PLUGIN_NAME_ENV"]
settings_path = Path(os.environ["SETTINGS_FILE_ENV"]).expanduser()
settings_path.parent.mkdir(parents=True, exist_ok=True)

data = {}
if settings_path.exists():
    try:
        data = json.loads(settings_path.read_text())
    except json.JSONDecodeError:
        data = {}

enabled = data.get("enabledPlugins") or {}
enabled[plugin_name] = True
data["enabledPlugins"] = enabled

settings_path.write_text(json.dumps(data, indent=2, sort_keys=False) + "\n")
PY
}

if [ -n "${MARKETPLACE_SOURCE}" ]; then
	if ! marketplace_list | grep -q "${MARKETPLACE_NAME}"; then
		echo "Adding marketplace ${MARKETPLACE_NAME} from ${MARKETPLACE_SOURCE}..."
		claude plugin marketplace add "${MARKETPLACE_SOURCE}"
	fi
else
	download_bundle
	if ! marketplace_list | grep -q "${MARKETPLACE_NAME}"; then
		echo "Adding marketplace ${MARKETPLACE_NAME} from ${INSTALL_DIR}..."
		claude plugin marketplace add "${INSTALL_DIR}"
	fi
fi

echo "Updating marketplace ${MARKETPLACE_NAME}..."
claude plugin marketplace update "${MARKETPLACE_NAME}" >/dev/null 2>&1 || true

echo "Installing ${PLUGIN_NAME}..."
claude plugin install "${PLUGIN_NAME}" --scope "${SCOPE}"

echo "Enabling ${PLUGIN_NAME}..."
if ensure_enabled_settings; then
	echo "Marked ${PLUGIN_NAME} enabled in ${SETTINGS_FILE}."
else
	echo "Could not update ${SETTINGS_FILE}; attempting CLI enable..."
	enable_plugin
fi

echo "Armour installed and enabled. Restart Claude Code to load the plugin."
