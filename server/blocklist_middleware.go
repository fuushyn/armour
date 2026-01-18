package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// cacheTTL is the time to live for the rules cache
	cacheTTL = 30 * time.Second
)

// BlocklistMiddleware handles blocklist enforcement for MCP requests
type BlocklistMiddleware struct {
	db         *sql.DB
	apiKey     string // For Claude API semantic matching
	stats      *StatsTracker
	rulesCache []BlocklistRule
	cacheMu    sync.RWMutex
	cacheTime  time.Time
	logger     Logger
}

// Logger interface for logging
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// noOpLogger is a no-op implementation of Logger
type noOpLogger struct{}

func (n *noOpLogger) Debug(format string, args ...interface{}) {}
func (n *noOpLogger) Info(format string, args ...interface{})  {}
func (n *noOpLogger) Warn(format string, args ...interface{})  {}
func (n *noOpLogger) Error(format string, args ...interface{}) {}

// NewBlocklistMiddleware creates a new blocklist middleware instance
func NewBlocklistMiddleware(db *sql.DB, apiKey string, stats *StatsTracker, logger Logger) *BlocklistMiddleware {
	if logger == nil {
		logger = &noOpLogger{}
	}
	return &BlocklistMiddleware{
		db:     db,
		apiKey: apiKey,
		stats:  stats,
		logger: logger,
	}
}

// Check validates if a requested operation on a tool is allowed
func (bm *BlocklistMiddleware) Check(method string, toolName string, args map[string]interface{}) (*BlocklistCheckResult, error) {
	// Get rules from cache or database
	rules, err := bm.getRules()
	if err != nil {
		bm.logger.Error("failed to get blocklist rules: %v", err)
		return &BlocklistCheckResult{Allowed: true}, nil // Fail open for safety
	}

	// Extract content from arguments for pattern matching
	content := bm.extractContent(method, toolName, args)

	// Check regex rules first (fast)
	if result := bm.checkRegexRules(content, toolName, method, rules); result != nil {
		return result, nil
	}

	// Check semantic rules (slow, uses API)
	if result := bm.checkSemanticRules(content, toolName, method, rules); result != nil {
		return result, nil
	}

	// No rules matched - allowed
	return &BlocklistCheckResult{Allowed: true}, nil
}

// getRules returns enabled blocklist rules, using cache if available
func (bm *BlocklistMiddleware) getRules() ([]BlocklistRule, error) {
	bm.cacheMu.RLock()
	now := time.Now()
	if !bm.cacheTime.IsZero() && now.Sub(bm.cacheTime) < cacheTTL && bm.rulesCache != nil {
		defer bm.cacheMu.RUnlock()
		return bm.rulesCache, nil
	}
	bm.cacheMu.RUnlock()

	// Refresh cache
	rules, err := GetEnabledBlocklistRules(bm.db)
	if err != nil {
		return nil, err
	}

	bm.cacheMu.Lock()
	bm.rulesCache = rules
	bm.cacheTime = time.Now()
	bm.cacheMu.Unlock()

	return rules, nil
}

// RefreshRulesCache forces a refresh of the rules cache
func (bm *BlocklistMiddleware) RefreshRulesCache() error {
	rules, err := GetEnabledBlocklistRules(bm.db)
	if err != nil {
		return err
	}

	bm.cacheMu.Lock()
	defer bm.cacheMu.Unlock()

	bm.rulesCache = rules
	bm.cacheTime = time.Now()

	return nil
}

// checkRegexRules checks if any regex rules match the content
func (bm *BlocklistMiddleware) checkRegexRules(content string, toolName string, method string, rules []BlocklistRule) *BlocklistCheckResult {
	for _, rule := range rules {
		// Skip non-regex rules
		if !rule.IsRegex {
			continue
		}

		// Check if rule applies to this tool
		if !RuleAppliesToTool(&rule, toolName) {
			continue
		}

		// Try to match the pattern
		matched, err := regexp.MatchString(rule.Pattern, content)
		if err != nil {
			bm.logger.Warn("invalid regex pattern in rule %d: %v", rule.ID, err)
			continue
		}

		if matched {
			bm.logger.Debug("regex rule %d matched: pattern=%s, tool=%s, method=%s",
				rule.ID, rule.Pattern, toolName, method)

			// Check permission for this method
			allowed, deniedOp := bm.checkPermission(&rule, method)
			if !allowed {
				bm.logger.Info("rule %d blocking %s on %s (action=%s)",
					rule.ID, deniedOp, toolName, rule.Action)

				if bm.stats != nil {
					bm.stats.RecordBlockedCall(toolName, fmt.Sprintf("regex_rule_%d:%s", rule.ID, rule.Pattern))
				}

				return &BlocklistCheckResult{
					Allowed:         false,
					DeniedOperation: deniedOp,
					MatchedRule:     &rule,
					Error: &MCPError{
						Code:    -32001,
						Message: fmt.Sprintf("Operation %s denied by blocklist rule: %s", deniedOp, rule.Description),
					},
				}
			}
		}
	}

	return nil
}

// checkSemanticRules checks if any semantic rules match the content using Claude API
func (bm *BlocklistMiddleware) checkSemanticRules(content string, toolName string, method string, rules []BlocklistRule) *BlocklistCheckResult {
	// Filter semantic rules
	var semanticRules []BlocklistRule
	for _, rule := range rules {
		if rule.IsSemantic && RuleAppliesToTool(&rule, toolName) {
			semanticRules = append(semanticRules, rule)
		}
	}

	if len(semanticRules) == 0 {
		return nil
	}

	// Extract topics from semantic rules
	topics := bm.extractTopics(semanticRules)
	if len(topics) == 0 {
		return nil
	}

	// Call Claude API for semantic matching
	matched, matchedTopic := bm.callClaudeAPI(topics, content)
	if matched {
		bm.logger.Debug("semantic rule matched: topic=%s, tool=%s", matchedTopic, toolName)

		// Find the matching rule
		var matchedRule *BlocklistRule
		for i := range semanticRules {
			if semanticRules[i].Pattern == matchedTopic {
				matchedRule = &semanticRules[i]
				break
			}
		}

		if matchedRule == nil {
			// Try matching by description
			for i := range semanticRules {
				if strings.Contains(semanticRules[i].Description, matchedTopic) {
					matchedRule = &semanticRules[i]
					break
				}
			}
		}

		if matchedRule != nil {
			// Check permission for this method
			allowed, deniedOp := bm.checkPermission(matchedRule, method)
			if !allowed {
				bm.logger.Info("semantic rule %d blocking %s on %s (topic=%s)",
					matchedRule.ID, deniedOp, toolName, matchedTopic)

				if bm.stats != nil {
					bm.stats.RecordBlockedCall(toolName, fmt.Sprintf("semantic_rule_%d:%s", matchedRule.ID, matchedTopic))
				}

				return &BlocklistCheckResult{
					Allowed:         false,
					DeniedOperation: deniedOp,
					MatchedRule:     matchedRule,
					Error: &MCPError{
						Code:    -32001,
						Message: fmt.Sprintf("Operation %s denied by blocklist rule: %s", deniedOp, matchedRule.Description),
					},
				}
			}
		}
	}

	return nil
}

// checkPermission checks if a method is allowed by a rule
func (bm *BlocklistMiddleware) checkPermission(rule *BlocklistRule, method string) (allowed bool, deniedOp string) {
	var perm Permission

	switch method {
	case "tools/call":
		perm = rule.Permissions.ToolsCall
	case "tools/list":
		perm = rule.Permissions.ToolsList
	case "resources/read":
		perm = rule.Permissions.ResourcesRead
	case "resources/list":
		perm = rule.Permissions.ResourcesList
	case "resources/subscribe":
		perm = rule.Permissions.ResourcesSubscribe
	case "prompts/get":
		perm = rule.Permissions.PromptsGet
	case "prompts/list":
		perm = rule.Permissions.PromptsList
	case "sampling/createMessage":
		perm = rule.Permissions.Sampling
	default:
		perm = PermissionInherit
	}

	// Evaluate permission
	if perm == PermissionDeny {
		return false, method // Explicitly denied
	} else if perm == PermissionAllow {
		return true, "" // Explicitly allowed
	}

	// Inherit: default deny for security
	return false, method
}

// extractContent extracts searchable content from request arguments
func (bm *BlocklistMiddleware) extractContent(method string, toolName string, args map[string]interface{}) string {
	var content strings.Builder
	content.WriteString(toolName)
	content.WriteString(" ")

	// Extract query/prompt/messages depending on the method
	if query, ok := args["query"].(string); ok {
		content.WriteString(query)
	} else if prompt, ok := args["prompt"].(string); ok {
		content.WriteString(prompt)
	} else if prompts, ok := args["prompts"].([]interface{}); ok {
		for _, p := range prompts {
			if pm, ok := p.(map[string]interface{}); ok {
				if text, ok := pm["text"].(string); ok {
					content.WriteString(" ")
					content.WriteString(text)
				}
			}
		}
	} else if text, ok := args["text"].(string); ok {
		content.WriteString(text)
	}

	return content.String()
}

// extractTopics extracts topic strings from semantic rules
func (bm *BlocklistMiddleware) extractTopics(rules []BlocklistRule) []string {
	topics := make(map[string]bool)

	for _, rule := range rules {
		if rule.IsSemantic {
			// Use pattern as topic if it looks like natural language
			if !strings.ContainsAny(rule.Pattern, "*[]()^$+?.\\|") {
				topics[rule.Pattern] = true
			}

			// Also add description as topic
			if rule.Description != "" {
				topics[rule.Description] = true
			}
		}
	}

	var result []string
	for topic := range topics {
		result = append(result, topic)
	}

	return result
}

// SemanticCheckResponse represents the response from Claude API semantic check
type SemanticCheckResponse struct {
	Blocked bool   `json:"blocked"`
	Topic   string `json:"topic"`
}

// callClaudeAPI calls the Claude API to check if content matches any blocked topics
func (bm *BlocklistMiddleware) callClaudeAPI(topics []string, content string) (matched bool, topic string) {
	if bm.apiKey == "" {
		return false, ""
	}

	// Truncate content for API call
	if len(content) > 1000 {
		content = content[:1000]
	}

	topicsStr := strings.Join(topics, ", ")
	prompt := fmt.Sprintf(`Analyze if this query relates to any of these blocked topics: %s

Query: %s

Respond with ONLY valid JSON: {"blocked": true/false, "topic": "matched topic or null"}`,
		topicsStr, content)

	payload := map[string]interface{}{
		"model": "claude-3-5-haiku-20241022",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		bm.logger.Error("failed to marshal payload: %v", err)
		return false, ""
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		bm.logger.Error("failed to create request: %v", err)
		return false, ""
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", bm.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		bm.logger.Error("failed to call Claude API: %v", err)
		return false, ""
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		bm.logger.Error("failed to read response: %v", err)
		return false, ""
	}

	if resp.StatusCode != http.StatusOK {
		bm.logger.Warn("Claude API returned status %d: %s", resp.StatusCode, string(respBody))
		return false, ""
	}

	// Parse the response
	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		bm.logger.Error("failed to unmarshal response: %v", err)
		return false, ""
	}

	if len(apiResp.Content) == 0 {
		bm.logger.Warn("empty response from Claude API")
		return false, ""
	}

	// Parse the JSON response from Claude
	var checkResp SemanticCheckResponse
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &checkResp); err != nil {
		bm.logger.Error("failed to parse Claude response: %v", err)
		return false, ""
	}

	if checkResp.Blocked && checkResp.Topic != "" && checkResp.Topic != "null" {
		return true, checkResp.Topic
	}

	return false, ""
}
