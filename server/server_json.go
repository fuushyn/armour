package server

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/mcp-go-proxy/proxy"
)

type serverJSONRemote struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type serverJSONManifest struct {
	Remotes []serverJSONRemote `json:"remotes"`
}

func parseServerJSONRemotes(pluginRoot string) []serverJSONRemote {
	if pluginRoot == "" {
		return nil
	}

	serverPath := filepath.Join(pluginRoot, "server.json")
	data, err := os.ReadFile(serverPath)
	if err != nil {
		return nil
	}

	var manifest serverJSONManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}

	remotes := make([]serverJSONRemote, 0, len(manifest.Remotes))
	for _, remote := range manifest.Remotes {
		if remote.URL == "" {
			continue
		}
		remotes = append(remotes, remote)
	}

	return remotes
}

func applyRemoteOverride(entry *proxy.ServerEntry, remote serverJSONRemote) {
	if entry == nil || remote.URL == "" {
		return
	}

	entry.URL = remote.URL
	if transport := normalizeRemoteTransport(remote); transport != "" {
		entry.Transport = transport
	}
}

func normalizeRemoteTransport(remote serverJSONRemote) string {
	if remote.Type != "" {
		switch strings.ToLower(remote.Type) {
		case "sse":
			return "sse"
		case "http":
			return "http"
		case "stdio":
			return "stdio"
		}
	}

	if strings.HasPrefix(remote.URL, "http://") || strings.HasPrefix(remote.URL, "https://") {
		return "http"
	}

	return ""
}

func sameHost(leftURL string, rightURL string) bool {
	if leftURL == "" || rightURL == "" {
		return false
	}

	leftParsed, err := url.Parse(leftURL)
	if err != nil {
		return false
	}

	rightParsed, err := url.Parse(rightURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(leftParsed.Host, rightParsed.Host)
}
