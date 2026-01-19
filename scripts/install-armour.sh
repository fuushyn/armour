#!/bin/bash
set -euo pipefail

MARKETPLACE_SOURCE="${ARMOUR_MARKETPLACE_SOURCE:-fuushyn/armour}"
MARKETPLACE_NAME="armour-marketplace"
PLUGIN_NAME="armour@${MARKETPLACE_NAME}"
SCOPE="user"

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

if ! marketplace_list | grep -q "${MARKETPLACE_NAME}"; then
  echo "Adding marketplace ${MARKETPLACE_NAME} from ${MARKETPLACE_SOURCE}..."
  claude plugin marketplace add "${MARKETPLACE_SOURCE}"
fi

echo "Updating marketplace ${MARKETPLACE_NAME}..."
claude plugin marketplace update "${MARKETPLACE_NAME}" >/dev/null 2>&1 || true

echo "Installing ${PLUGIN_NAME}..."
claude plugin install "${PLUGIN_NAME}" --scope "${SCOPE}"

echo "Enabling ${PLUGIN_NAME}..."
enable_plugin

echo "Armour installed and enabled. Restart Claude Code to load the plugin."
