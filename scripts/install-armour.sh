#!/bin/bash
set -euo pipefail

MARKETPLACE_SOURCE="fuushyn/armour"
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

if ! marketplace_list | grep -q "${MARKETPLACE_NAME}"; then
  echo "Adding marketplace ${MARKETPLACE_NAME} from ${MARKETPLACE_SOURCE}..."
  claude plugin marketplace add "${MARKETPLACE_SOURCE}"
fi

echo "Updating marketplace ${MARKETPLACE_NAME}..."
claude plugin marketplace update "${MARKETPLACE_NAME}" >/dev/null 2>&1 || true

echo "Installing ${PLUGIN_NAME}..."
claude plugin install "${PLUGIN_NAME}" --scope "${SCOPE}"

echo "Enabling ${PLUGIN_NAME}..."
claude plugin enable "${PLUGIN_NAME}" --scope "${SCOPE}"

echo "Armour installed and enabled. Restart Claude Code to load the plugin."
