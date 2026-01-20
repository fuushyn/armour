package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/user/mcp-go-proxy/proxy"
)

var envDefaultPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-|-)([^}]*)\}`)

func applyPluginContext(entry *proxy.ServerEntry, pluginName string, pluginRoot string) {
	if entry.Env == nil {
		entry.Env = make(map[string]string)
	}
	if pluginRoot != "" {
		if _, ok := entry.Env["CLAUDE_PLUGIN_ROOT"]; !ok {
			entry.Env["CLAUDE_PLUGIN_ROOT"] = pluginRoot
		}
	}
	if pluginName != "" {
		if _, ok := entry.Env["CLAUDE_PLUGIN_NAME"]; !ok {
			entry.Env["CLAUDE_PLUGIN_NAME"] = pluginName
		}
	}
}

func expandEnvString(value string, lookup func(string) string) string {
	if value == "" {
		return value
	}

	expanded := envDefaultPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := envDefaultPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		key := parts[1]
		fallback := parts[3]
		if val := lookup(key); val != "" {
			return val
		}
		return fallback
	})

	return os.Expand(expanded, func(key string) string {
		if lookup == nil {
			return ""
		}
		return lookup(key)
	})
}

func expandServerEntry(entry *proxy.ServerEntry) {
	if entry == nil {
		return
	}

	lookup := func(key string) string {
		if entry.Env != nil {
			if val, ok := entry.Env[key]; ok {
				return val
			}
		}
		return os.Getenv(key)
	}

	expand := func(value string) string {
		return expandEnvString(value, lookup)
	}

	if entry.Command != "" {
		entry.Command = expand(entry.Command)
	}
	if entry.URL != "" {
		entry.URL = expand(entry.URL)
	}
	for i, arg := range entry.Args {
		entry.Args[i] = expand(arg)
	}
	if entry.Headers != nil {
		for key, value := range entry.Headers {
			entry.Headers[key] = expand(value)
		}
	}
	if entry.Env != nil {
		for key, value := range entry.Env {
			entry.Env[key] = expand(value)
		}
	}

	pluginRoot := ""
	if entry.Env != nil {
		pluginRoot = entry.Env["CLAUDE_PLUGIN_ROOT"]
	}
	if pluginRoot == "" {
		return
	}

	if entry.Command != "" && strings.HasPrefix(entry.Command, ".") {
		entry.Command = filepath.Join(pluginRoot, entry.Command)
	}
	for i, arg := range entry.Args {
		if strings.HasPrefix(arg, ".") {
			entry.Args[i] = filepath.Join(pluginRoot, arg)
		}
	}
}
