package server

import (
	"database/sql"
	"fmt"
)

// MigrateHardcodedPatternsToBlocklist converts hardcoded destructive patterns to database blocklist rules
func MigrateHardcodedPatternsToBlocklist(db *sql.DB) error {
	// List of destructive patterns from the original hardcoded list in policy_manager.go
	destructivePatterns := []string{
		"rm", "delete", "drop", "truncate", "destroy", "wipe",
		"remove", "erase", "kill", "terminate", "uninstall",
		"format", "reset", "clear", "flush", "purge",
	}

	for _, pattern := range destructivePatterns {
		rule := &BlocklistRule{
			Pattern:     pattern + "*",
			Description: fmt.Sprintf("Migrated destructive pattern: %s", pattern),
			Action:      "block",
			IsRegex:     true,
			IsSemantic:  false,
			Tools:       "", // Apply to all tools
			Enabled:     true,
			Permissions: Permissions{
				ToolsCall:         PermissionDeny,
				ToolsList:         PermissionAllow,
				ResourcesRead:     PermissionDeny,
				ResourcesList:     PermissionAllow,
				ResourcesSubscribe: PermissionDeny,
				PromptsGet:        PermissionDeny,
				PromptsList:       PermissionAllow,
				Sampling:          PermissionDeny,
			},
		}

		if err := CreateBlocklistRule(db, rule); err != nil {
			return fmt.Errorf("failed to migrate pattern %s: %w", pattern, err)
		}
	}

	return nil
}

// MigrateStrictModeToBlocklist creates blocklist rules equivalent to strict mode
// Strict mode blocks everything except whitelist
func MigrateStrictModeToBlocklist(db *sql.DB) error {
	// Create a rule that blocks all tool calls by default
	strictRule := &BlocklistRule{
		Pattern:     ".*", // Match everything
		Description: "Migrated strict mode: blocks all tool calls by default",
		Action:      "block",
		IsRegex:     true,
		IsSemantic:  false,
		Tools:       "",
		Enabled:     false, // Disabled by default - admin must enable if needed
		Permissions: Permissions{
			ToolsCall:         PermissionDeny,
			ToolsList:         PermissionAllow,
			ResourcesRead:     PermissionDeny,
			ResourcesList:     PermissionAllow,
			ResourcesSubscribe: PermissionDeny,
			PromptsGet:        PermissionDeny,
			PromptsList:       PermissionAllow,
			Sampling:          PermissionDeny,
		},
	}

	if err := CreateBlocklistRule(db, strictRule); err != nil {
		return fmt.Errorf("failed to create strict mode rule: %w", err)
	}

	return nil
}

// MigratePermissiveModeToBlocklist creates a permissive rule that allows everything
// This rule is disabled by default
func MigratePermissiveModeToBlocklist(db *sql.DB) error {
	// Create a rule that allows all operations
	permissiveRule := &BlocklistRule{
		Pattern:     ".*", // Match everything
		Description: "Migrated permissive mode: allows all operations (disabled by default)",
		Action:      "allow",
		IsRegex:     true,
		IsSemantic:  false,
		Tools:       "",
		Enabled:     false, // Disabled by default for security
		Permissions: Permissions{
			ToolsCall:         PermissionAllow,
			ToolsList:         PermissionAllow,
			ResourcesRead:     PermissionAllow,
			ResourcesList:     PermissionAllow,
			ResourcesSubscribe: PermissionAllow,
			PromptsGet:        PermissionAllow,
			PromptsList:       PermissionAllow,
			Sampling:          PermissionAllow,
		},
	}

	if err := CreateBlocklistRule(db, permissiveRule); err != nil {
		return fmt.Errorf("failed to create permissive mode rule: %w", err)
	}

	return nil
}

// MigrateModerateModeToBlocklist creates blocklist rules equivalent to moderate mode
// Moderate mode allows most things but blocks destructive patterns
func MigrateModerateModeToBlocklist(db *sql.DB) error {
	// Migrate the hardcoded destructive patterns (already done in MigrateHardcodedPatternsToBlocklist)
	// No additional rule needed - moderate mode is the default behavior
	return nil
}

// PerformFullMigration runs all migrations from policy-based system to blocklist-based system
func PerformFullMigration(db *sql.DB) error {
	// 1. Migrate hardcoded destructive patterns
	if err := MigrateHardcodedPatternsToBlocklist(db); err != nil {
		return fmt.Errorf("failed to migrate hardcoded patterns: %w", err)
	}

	// 2. Create rules for different policy modes (optional, disabled by default)
	if err := MigrateStrictModeToBlocklist(db); err != nil {
		return fmt.Errorf("failed to migrate strict mode: %w", err)
	}

	if err := MigratePermissiveModeToBlocklist(db); err != nil {
		return fmt.Errorf("failed to migrate permissive mode: %w", err)
	}

	// Moderate mode doesn't need special handling - it's the default

	return nil
}

// CheckIfMigrationNeeded checks if the blocklist_rules table has any data
// Returns true if migration is needed (table is empty)
func CheckIfMigrationNeeded(db *sql.DB) (bool, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM blocklist_rules").Scan(&count)
	if err != nil {
		return false, err
	}

	return count == 0, nil
}
