package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// communityRule represents a lightweight rule definition that can be loaded from disk or remote.
type communityRule struct {
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Tools       string `json:"tools"`
	IsRegex     bool   `json:"is_regex"`
	BlockAll    bool   `json:"block_all"`
}

// loadCommunityRules loads rules from a set of file or URL sources.
func loadCommunityRules(sources []string, logger Logger) []BlocklistRule {
	var rules []BlocklistRule

	for _, src := range sources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}

		data, err := fetchSource(src)
		if err != nil {
			logger.Warn("community blocklist: failed to load %s: %v", src, err)
			continue
		}

		var parsed []communityRule
		if err := json.Unmarshal(data, &parsed); err != nil {
			logger.Warn("community blocklist: %s invalid JSON: %v", src, err)
			continue
		}

		for _, cr := range parsed {
			action := strings.ToLower(cr.Action)
			if action == "" {
				action = "block"
			}
			rules = append(rules, BlocklistRule{
				Pattern:     cr.Pattern,
				Description: fallbackString(cr.Description, "community rule"),
				Action:      action,
				IsRegex:     cr.IsRegex || cr.BlockAll,
				Tools:       strings.TrimSpace(cr.Tools),
				Permissions: DefaultPermissions(action),
				Enabled:     true,
			})
		}
	}

	return rules
}

func fetchSource(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}

	return os.ReadFile(src)
}

func fallbackString(value, defaultVal string) string {
	if strings.TrimSpace(value) == "" {
		return defaultVal
	}
	return value
}

// defaultCommunitySources returns env or default paths.
func defaultCommunitySources() []string {
	var sources []string
	if env := os.Getenv("ARMOUR_BLOCKLIST_SOURCES"); env != "" {
		for _, part := range strings.Split(env, ",") {
			sources = append(sources, strings.TrimSpace(part))
		}
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		defaultDir := filepath.Join(homeDir, ".armour", "blocklists.d")
		if entries, err := os.ReadDir(defaultDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), ".json") {
					sources = append(sources, filepath.Join(defaultDir, entry.Name()))
				}
			}
		}
	}

	return sources
}
