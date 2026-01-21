package server

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ensureBlocklistSchema makes sure the blocklist table exists before queries.
// It reuses initDB which is idempotent (CREATE TABLE IF NOT EXISTS).
func ensureBlocklistSchema(db *sql.DB) error {
	if err := initDB(db); err != nil {
		return fmt.Errorf("failed to ensure blocklist schema: %w", err)
	}
	return nil
}

// GetEnabledBlocklistRules retrieves all enabled blocklist rules from the database
func GetEnabledBlocklistRules(db *sql.DB) ([]BlocklistRule, error) {
	if err := ensureBlocklistSchema(db); err != nil {
		return nil, err
	}

	query := `
		SELECT id, pattern, description, action, is_regex, is_semantic, tools,
		       perm_tools_call, perm_tools_list, perm_resources_read, perm_resources_list,
		       perm_resources_subscribe, perm_prompts_get, perm_prompts_list, perm_sampling,
		       enabled, created_at, updated_at
		FROM blocklist_rules
		WHERE enabled = 1
		ORDER BY id ASC
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query blocklist rules: %w", err)
	}
	defer rows.Close()

	var rules []BlocklistRule
	for rows.Next() {
		var rule BlocklistRule
		var perms Permissions

		err := rows.Scan(
			&rule.ID, &rule.Pattern, &rule.Description, &rule.Action,
			&rule.IsRegex, &rule.IsSemantic, &rule.Tools,
			&perms.ToolsCall, &perms.ToolsList, &perms.ResourcesRead,
			&perms.ResourcesList, &perms.ResourcesSubscribe,
			&perms.PromptsGet, &perms.PromptsList, &perms.Sampling,
			&rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blocklist rule: %w", err)
		}

		rule.Permissions = perms
		rules = append(rules, rule)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blocklist rules: %w", err)
	}

	return rules, nil
}

// GetAllBlocklistRules retrieves all blocklist rules (including disabled) from the database
func GetAllBlocklistRules(db *sql.DB) ([]BlocklistRule, error) {
	if err := ensureBlocklistSchema(db); err != nil {
		return nil, err
	}

	query := `
		SELECT id, pattern, description, action, is_regex, is_semantic, tools,
		       perm_tools_call, perm_tools_list, perm_resources_read, perm_resources_list,
		       perm_resources_subscribe, perm_prompts_get, perm_prompts_list, perm_sampling,
		       enabled, created_at, updated_at
		FROM blocklist_rules
		ORDER BY id ASC
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query blocklist rules: %w", err)
	}
	defer rows.Close()

	var rules []BlocklistRule
	for rows.Next() {
		var rule BlocklistRule
		var perms Permissions

		err := rows.Scan(
			&rule.ID, &rule.Pattern, &rule.Description, &rule.Action,
			&rule.IsRegex, &rule.IsSemantic, &rule.Tools,
			&perms.ToolsCall, &perms.ToolsList, &perms.ResourcesRead,
			&perms.ResourcesList, &perms.ResourcesSubscribe,
			&perms.PromptsGet, &perms.PromptsList, &perms.Sampling,
			&rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blocklist rule: %w", err)
		}

		rule.Permissions = perms
		rules = append(rules, rule)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blocklist rules: %w", err)
	}

	return rules, nil
}

// CreateBlocklistRule inserts a new blocklist rule into the database
func CreateBlocklistRule(db *sql.DB, rule *BlocklistRule) error {
	if err := ensureBlocklistSchema(db); err != nil {
		return err
	}

	query := `
		INSERT INTO blocklist_rules (
			pattern, description, action, is_regex, is_semantic, tools,
			perm_tools_call, perm_tools_list, perm_resources_read, perm_resources_list,
			perm_resources_subscribe, perm_prompts_get, perm_prompts_list, perm_sampling,
			enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	now := time.Now()
	result, err := db.Exec(
		query,
		rule.Pattern, rule.Description, rule.Action, rule.IsRegex, rule.IsSemantic, rule.Tools,
		rule.Permissions.ToolsCall, rule.Permissions.ToolsList, rule.Permissions.ResourcesRead,
		rule.Permissions.ResourcesList, rule.Permissions.ResourcesSubscribe,
		rule.Permissions.PromptsGet, rule.Permissions.PromptsList, rule.Permissions.Sampling,
		rule.Enabled, now, now,
	)

	if err != nil {
		return fmt.Errorf("failed to insert blocklist rule: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now

	return nil
}

// UpdateBlocklistRule updates an existing blocklist rule in the database
func UpdateBlocklistRule(db *sql.DB, rule *BlocklistRule) error {
	if err := ensureBlocklistSchema(db); err != nil {
		return err
	}

	query := `
		UPDATE blocklist_rules
		SET pattern = ?, description = ?, action = ?, is_regex = ?, is_semantic = ?, tools = ?,
		    perm_tools_call = ?, perm_tools_list = ?, perm_resources_read = ?, perm_resources_list = ?,
		    perm_resources_subscribe = ?, perm_prompts_get = ?, perm_prompts_list = ?, perm_sampling = ?,
		    enabled = ?, updated_at = ?
		WHERE id = ?
	`

	now := time.Now()
	result, err := db.Exec(
		query,
		rule.Pattern, rule.Description, rule.Action, rule.IsRegex, rule.IsSemantic, rule.Tools,
		rule.Permissions.ToolsCall, rule.Permissions.ToolsList, rule.Permissions.ResourcesRead,
		rule.Permissions.ResourcesList, rule.Permissions.ResourcesSubscribe,
		rule.Permissions.PromptsGet, rule.Permissions.PromptsList, rule.Permissions.Sampling,
		rule.Enabled, now, rule.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update blocklist rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("blocklist rule not found: id=%d", rule.ID)
	}

	rule.UpdatedAt = now

	return nil
}

// DeleteBlocklistRule deletes a blocklist rule from the database
func DeleteBlocklistRule(db *sql.DB, id int64) error {
	if err := ensureBlocklistSchema(db); err != nil {
		return err
	}

	query := `DELETE FROM blocklist_rules WHERE id = ?`

	result, err := db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete blocklist rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("blocklist rule not found: id=%d", id)
	}

	return nil
}

// ToggleBlocklistRule enables or disables a blocklist rule
func ToggleBlocklistRule(db *sql.DB, id int64) error {
	if err := ensureBlocklistSchema(db); err != nil {
		return err
	}

	query := `
		UPDATE blocklist_rules
		SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END,
		    updated_at = ?
		WHERE id = ?
	`

	result, err := db.Exec(query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to toggle blocklist rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("blocklist rule not found: id=%d", id)
	}

	return nil
}

// GetBlocklistRuleByID retrieves a single blocklist rule by ID
func GetBlocklistRuleByID(db *sql.DB, id int64) (*BlocklistRule, error) {
	if err := ensureBlocklistSchema(db); err != nil {
		return nil, err
	}

	query := `
		SELECT id, pattern, description, action, is_regex, is_semantic, tools,
		       perm_tools_call, perm_tools_list, perm_resources_read, perm_resources_list,
		       perm_resources_subscribe, perm_prompts_get, perm_prompts_list, perm_sampling,
		       enabled, created_at, updated_at
		FROM blocklist_rules
		WHERE id = ?
	`

	var rule BlocklistRule
	var perms Permissions

	err := db.QueryRow(query, id).Scan(
		&rule.ID, &rule.Pattern, &rule.Description, &rule.Action,
		&rule.IsRegex, &rule.IsSemantic, &rule.Tools,
		&perms.ToolsCall, &perms.ToolsList, &perms.ResourcesRead,
		&perms.ResourcesList, &perms.ResourcesSubscribe,
		&perms.PromptsGet, &perms.PromptsList, &perms.Sampling,
		&rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("blocklist rule not found: id=%d", id)
		}
		return nil, fmt.Errorf("failed to query blocklist rule: %w", err)
	}

	rule.Permissions = perms

	return &rule, nil
}

// LogBlocklistMatch logs a blocklist rule match to the audit log
func LogBlocklistMatch(
	db *sql.DB,
	method, toolName, content, deniedOp, ruleAction string,
	matchedPattern string,
	userID, sessionID *string,
) error {
	if err := ensureBlocklistSchema(db); err != nil {
		return err
	}

	query := `
		INSERT INTO audit_log (
			user_id, agent_id, server_id, method, capability, session_id,
			blocked, block_reason, matched_pattern, denied_operation, rule_action,
			timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Truncate content to first 500 chars
	if len(content) > 500 {
		content = content[:500]
	}

	_, err := db.Exec(
		query,
		userID, nil, "armour-proxy", method, method, sessionID,
		1, "blocklist_match", matchedPattern, deniedOp, ruleAction,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf("failed to log blocklist match: %w", err)
	}

	return nil
}

// ExtractToolNames parses a comma-separated string of tool names
func ExtractToolNames(toolsStr string) []string {
	if toolsStr == "" {
		return []string{}
	}

	var names []string
	for _, name := range strings.Split(toolsStr, ",") {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// RuleAppliesToTool checks if a rule applies to a specific tool name
func RuleAppliesToTool(rule *BlocklistRule, toolName string) bool {
	if rule.Tools == "" || rule.Tools == "*" {
		return true // Apply to all tools
	}

	toolName = strings.ToLower(toolName)
	// Normalize tool name: replace __ and : with common separator for matching
	normalizedToolName := strings.ReplaceAll(toolName, "__", "_")
	normalizedToolName = strings.ReplaceAll(normalizedToolName, ":", "_")

	toolPatterns := ExtractToolNames(rule.Tools)
	for _, t := range toolPatterns {
		t = strings.ToLower(t)
		// Normalize pattern too
		normalizedPattern := strings.ReplaceAll(t, "__", "_")
		normalizedPattern = strings.ReplaceAll(normalizedPattern, ":", "_")

		if t == toolName || normalizedPattern == normalizedToolName {
			return true
		}
		// Contains match (e.g., "*delete*" matches "file_delete_doc")
		if strings.HasPrefix(t, "*") && strings.HasSuffix(t, "*") && len(t) > 2 {
			keyword := t[1 : len(t)-1]
			if strings.Contains(toolName, keyword) || strings.Contains(normalizedToolName, keyword) {
				return true
			}
		}
		// Wildcard suffix match (e.g., "*delete" matches "file_delete")
		if strings.HasPrefix(t, "*") && !strings.HasSuffix(t, "*") {
			suffix := t[1:]
			if strings.HasSuffix(toolName, suffix) || strings.HasSuffix(normalizedToolName, suffix) {
				return true
			}
		}
		// Wildcard prefix match (e.g., "bash*" matches "bash_exec")
		if strings.HasSuffix(t, "*") && !strings.HasPrefix(t, "*") {
			prefix := t[:len(t)-1]
			if strings.HasPrefix(toolName, prefix) || strings.HasPrefix(normalizedToolName, prefix) {
				return true
			}
		}
		// Plain keyword contains match (no wildcards) - check if keyword appears in tool name
		if !strings.Contains(t, "*") {
			if strings.Contains(toolName, t) || strings.Contains(normalizedToolName, t) {
				return true
			}
		}
	}

	return false
}
