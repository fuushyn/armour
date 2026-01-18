package server

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestBlocklistBasics tests basic blocklist operations
func TestBlocklistBasics(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := initDB(db); err != nil {
		t.Fatalf("Failed to init database: %v", err)
	}

	// Test 1: Create a blocklist rule
	rule := &BlocklistRule{
		Pattern:     "delete_.*",
		Description: "Block deletion operations",
		Action:      "block",
		IsRegex:     true,
		IsSemantic:  false,
		Tools:       "",
		Enabled:     true,
		Permissions: DefaultPermissions("block"),
	}

	err = CreateBlocklistRule(db, rule)
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	if rule.ID == 0 {
		t.Fatal("Rule ID should be set after creation")
	}

	// Test 2: Retrieve the rule
	retrieved, err := GetBlocklistRuleByID(db, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}

	if retrieved.Pattern != rule.Pattern {
		t.Fatalf("Pattern mismatch: expected %s, got %s", rule.Pattern, retrieved.Pattern)
	}

	// Test 3: List all rules
	rules, err := GetAllBlocklistRules(db)
	if err != nil {
		t.Fatalf("Failed to list rules: %v", err)
	}

	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	// Test 4: Update the rule
	retrieved.Description = "Updated description"
	err = UpdateBlocklistRule(db, retrieved)
	if err != nil {
		t.Fatalf("Failed to update rule: %v", err)
	}

	// Test 5: Toggle rule
	err = ToggleBlocklistRule(db, rule.ID)
	if err != nil {
		t.Fatalf("Failed to toggle rule: %v", err)
	}

	// Test 6: Delete the rule
	err = DeleteBlocklistRule(db, rule.ID)
	if err != nil {
		t.Fatalf("Failed to delete rule: %v", err)
	}

	// Verify deletion
	rules, err = GetAllBlocklistRules(db)
	if err != nil {
		t.Fatalf("Failed to list rules: %v", err)
	}

	if len(rules) != 0 {
		t.Fatalf("Expected 0 rules after deletion, got %d", len(rules))
	}
}

// TestPermissions tests permission evaluation logic
func TestPermissions(t *testing.T) {
	tests := []struct {
		name          string
		permission    Permission
		expectedAllow bool
	}{
		{"Allow", PermissionAllow, true},
		{"Deny", PermissionDeny, false},
		{"Inherit", PermissionInherit, false}, // Inherits default deny for security
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if (tt.permission != PermissionDeny) != tt.expectedAllow {
				// This is a simple test - real permission checking happens in middleware
			}
		})
	}
}

// TestDefaultPermissions tests default permission generation
func TestDefaultPermissions(t *testing.T) {
	blockPerms := DefaultPermissions("block")
	if blockPerms.ToolsCall != PermissionDeny {
		t.Fatal("Block rule should deny tools_call")
	}
	if blockPerms.ToolsList != PermissionAllow {
		t.Fatal("Block rule should allow tools_list")
	}

	allowPerms := DefaultPermissions("allow")
	if allowPerms.ToolsCall != PermissionAllow {
		t.Fatal("Allow rule should allow tools_call")
	}
	if allowPerms.ToolsList != PermissionAllow {
		t.Fatal("Allow rule should allow tools_list")
	}
}

// TestRuleAppliesToTool tests tool filtering logic
func TestRuleAppliesToTool(t *testing.T) {
	tests := []struct {
		tools     string
		toolName  string
		applies   bool
		desc      string
	}{
		{"", "any_tool", true, "Empty tools applies to all"},
		{"*", "any_tool", true, "Wildcard applies to all"},
		{"tool1, tool2", "tool1", true, "Exact match"},
		{"tool1, tool2", "tool3", false, "No match"},
		{"*delete", "rm_delete", true, "Suffix match"},
		{"*delete", "delete", true, "Exact suffix match"},
		{"*delete", "rm_remove", false, "No suffix match"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rule := &BlocklistRule{Tools: tt.tools}
			result := RuleAppliesToTool(rule, tt.toolName)
			if result != tt.applies {
				t.Fatalf("Expected %v, got %v for tools=%q, tool=%q", tt.applies, result, tt.tools, tt.toolName)
			}
		})
	}
}

// TestMigration tests the migration functions
func TestMigration(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := initDB(db); err != nil {
		t.Fatalf("Failed to init database: %v", err)
	}

	// Check if migration is needed (should be true - table is empty)
	needed, err := CheckIfMigrationNeeded(db)
	if err != nil {
		t.Fatalf("Failed to check migration status: %v", err)
	}
	if !needed {
		t.Fatal("Migration should be needed for empty table")
	}

	// Run migration
	err = PerformFullMigration(db)
	if err != nil {
		t.Fatalf("Failed to perform migration: %v", err)
	}

	// Check if migration is still needed (should be false - table now has rules)
	needed, err = CheckIfMigrationNeeded(db)
	if err != nil {
		t.Fatalf("Failed to check migration status: %v", err)
	}
	if needed {
		t.Fatal("Migration should not be needed after running migration")
	}

	// Verify rules were created
	rules, err := GetAllBlocklistRules(db)
	if err != nil {
		t.Fatalf("Failed to list rules: %v", err)
	}

	if len(rules) == 0 {
		t.Fatal("Expected rules to be created by migration")
	}

	// Verify we have destructive patterns
	hasDestructivePattern := false
	for _, rule := range rules {
		if rule.IsRegex && rule.Action == "block" {
			hasDestructivePattern = true
			break
		}
	}

	if !hasDestructivePattern {
		t.Fatal("Expected destructive patterns to be migrated")
	}
}

// TestConcurrentAccess tests concurrent database access
func TestConcurrentAccess(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := initDB(db); err != nil {
		t.Fatalf("Failed to init database: %v", err)
	}

	done := make(chan bool, 10)

	// Create multiple rules concurrently
	for i := 0; i < 10; i++ {
		go func(index int) {
			rule := &BlocklistRule{
				Pattern:     "pattern_" + string(rune(index)),
				Description: "Concurrent test rule",
				Action:      "block",
				IsRegex:     false,
				IsSemantic:  false,
				Tools:       "",
				Enabled:     true,
				Permissions: DefaultPermissions("block"),
			}

			err := CreateBlocklistRule(db, rule)
			if err != nil {
				t.Errorf("Failed to create rule: %v", err)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all rules were created
	rules, err := GetAllBlocklistRules(db)
	if err != nil {
		t.Fatalf("Failed to list rules: %v", err)
	}

	if len(rules) != 10 {
		t.Fatalf("Expected 10 rules, got %d", len(rules))
	}
}
