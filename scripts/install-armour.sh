#!/bin/bash
set -euo pipefail

MARKETPLACE_SOURCE="${ARMOUR_MARKETPLACE_SOURCE:-fuushyn/armour}"
MARKETPLACE_NAME="armour-marketplace"
PLUGIN_NAME="armour@${MARKETPLACE_NAME}"
SCOPE="user"
SETTINGS_FILE="${CLAUDE_SETTINGS_PATH:-$HOME/.claude/settings.json}"

if ! command -v claude >/dev/null 2>&1; then
  echo "Claude Code CLI not found. Install Claude Code first."
  exit 1
fi

marketplace_list() {
  claude plugin marketplace list 2>/dev/null
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

if ! marketplace_list | grep -q "${MARKETPLACE_NAME}"; then
	echo "Adding marketplace ${MARKETPLACE_NAME} from ${MARKETPLACE_SOURCE}..."
	claude plugin marketplace add "${MARKETPLACE_SOURCE}"
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
