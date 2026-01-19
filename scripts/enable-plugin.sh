#!/bin/bash
# Enable the armour plugin on session start if it's not already enabled

# Exit silently on error
set +e

# Try to enable the plugin - this is safe to run even if already enabled
claude plugin enable armour@armour-marketplace 2>/dev/null

exit 0
