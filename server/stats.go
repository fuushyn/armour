package server

import (
	"fmt"
	"sync"
	"time"
)

// StatsTracker tracks KPIs like blocked tool calls for the GitHub dashboard.
type StatsTracker struct {
	// Counters
	blockedCallsTotal  int64             // Total destructive calls blocked
	allowedCallsTotal  int64             // Total calls allowed
	blockedToolsCount  map[string]int64  // Count per tool name
	allowedToolsCount  map[string]int64  // Count per tool name
	blockedByReason    map[string]int64  // Count by blocking reason (strict_mode, policy, destructive)

	// Time-series data
	dailyStats map[string]*DailyStats   // YYYY-MM-DD -> stats

	// Configuration
	startTime time.Time
	mu        sync.RWMutex
}

// DailyStats tracks stats for a single day.
type DailyStats struct {
	Date             string
	BlockedCalls     int64
	AllowedCalls     int64
	UniqueTools      map[string]int64
	BlockingReasons  map[string]int64
}

// NewStatsTracker creates a new stats tracker.
func NewStatsTracker() *StatsTracker {
	return &StatsTracker{
		blockedToolsCount: make(map[string]int64),
		allowedToolsCount: make(map[string]int64),
		blockedByReason:   make(map[string]int64),
		dailyStats:        make(map[string]*DailyStats),
		startTime:         time.Now(),
	}
}

// RecordBlockedCall records a tool call that was blocked.
func (st *StatsTracker) RecordBlockedCall(toolName string, reason string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.blockedCallsTotal++
	st.blockedToolsCount[toolName]++
	st.blockedByReason[reason]++

	// Update daily stats
	today := time.Now().Format("2006-01-02")
	if st.dailyStats[today] == nil {
		st.dailyStats[today] = &DailyStats{
			Date:            today,
			UniqueTools:     make(map[string]int64),
			BlockingReasons: make(map[string]int64),
		}
	}

	st.dailyStats[today].BlockedCalls++
	st.dailyStats[today].UniqueTools[toolName]++
	st.dailyStats[today].BlockingReasons[reason]++
}

// RecordAllowedCall records a tool call that was allowed.
func (st *StatsTracker) RecordAllowedCall(toolName string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.allowedCallsTotal++
	st.allowedToolsCount[toolName]++

	// Update daily stats
	today := time.Now().Format("2006-01-02")
	if st.dailyStats[today] == nil {
		st.dailyStats[today] = &DailyStats{
			Date:            today,
			UniqueTools:     make(map[string]int64),
			BlockingReasons: make(map[string]int64),
		}
	}

	st.dailyStats[today].AllowedCalls++
	st.dailyStats[today].UniqueTools[toolName]++
}

// GetStats returns the current aggregate statistics.
func (st *StatsTracker) GetStats() StatsSnapshot {
	st.mu.RLock()
	defer st.mu.RUnlock()

	totalCalls := st.blockedCallsTotal + st.allowedCallsTotal
	blockRate := 0.0
	if totalCalls > 0 {
		blockRate = float64(st.blockedCallsTotal) / float64(totalCalls) * 100
	}

	return StatsSnapshot{
		Timestamp:          time.Now().Unix(),
		BlockedCallsTotal:  st.blockedCallsTotal,
		AllowedCallsTotal:  st.allowedCallsTotal,
		TotalCalls:         totalCalls,
		BlockRate:          blockRate,
		UniqueBlockedTools: len(st.blockedToolsCount),
		UniqueAllowedTools: len(st.allowedToolsCount),
		BlockedByReason:    st.copyMap(st.blockedByReason),
		TopBlockedTools:    st.topTools(st.blockedToolsCount, 5),
		TopAllowedTools:    st.topTools(st.allowedToolsCount, 5),
		Uptime:             time.Since(st.startTime).Seconds(),
	}
}

// GetDailyStats returns stats for a specific day.
func (st *StatsTracker) GetDailyStats(date string) *DailyStats {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if stats, exists := st.dailyStats[date]; exists {
		return stats
	}
	return nil
}

// GetWeeklyStats returns aggregated stats for the past week.
func (st *StatsTracker) GetWeeklyStats() WeeklyStats {
	st.mu.RLock()
	defer st.mu.RUnlock()

	stats := WeeklyStats{
		Period:        "Last 7 Days",
		Days:          make([]DailyStats, 0),
		TotalBlocked:  0,
		TotalAllowed:  0,
		TopBlockedTools: make([]ToolStat, 0),
	}

	topTools := make(map[string]int64)

	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if dayStats, exists := st.dailyStats[date]; exists {
			stats.Days = append(stats.Days, *dayStats)
			stats.TotalBlocked += dayStats.BlockedCalls
			stats.TotalAllowed += dayStats.AllowedCalls

			for tool, count := range dayStats.UniqueTools {
				topTools[tool] += count
			}
		}
	}

	// Get top tools
	for tool, count := range topTools {
		stats.TopBlockedTools = append(stats.TopBlockedTools, ToolStat{
			Name:  tool,
			Count: count,
		})
	}

	return stats
}

// GetGitHubBadgeMarkdown returns markdown for a GitHub badge showing blocked calls.
func (st *StatsTracker) GetGitHubBadgeMarkdown() string {
	st.mu.RLock()
	defer st.mu.RUnlock()

	blockedStr := fmt.Sprintf("%d", st.blockedCallsTotal)
	color := "brightgreen"

	if st.blockedCallsTotal == 0 {
		color = "lightgrey"
		blockedStr = "0"
	} else if st.blockedCallsTotal > 1000 {
		color = "orange"
	}

	// Shields.io badge URL
	badgeURL := fmt.Sprintf(
		"https://img.shields.io/badge/Destructive_Calls_Blocked-%s-%s?style=flat-square",
		blockedStr,
		color,
	)

	return fmt.Sprintf(
		"[![Destructive Calls Blocked](%s)](https://github.com/yourusername/mcp-go-proxy)",
		badgeURL,
	)
}

// GetGitHubREADMEStats returns formatted stats for GitHub README.
func (st *StatsTracker) GetGitHubREADMEStats() string {
	stats := st.GetStats()

	output := fmt.Sprintf(`
## Security KPIs

> **This proxy has prevented %d destructive tool calls from executing.**

| Metric | Value |
|--------|-------|
| Destructive Calls Blocked | %d |
| Allowed Calls | %d |
| Block Rate | %.1f%% |
| Unique Blocked Tools | %d |
| Uptime | %.0f hours |
`,
		stats.BlockedCallsTotal,
		stats.BlockedCallsTotal,
		stats.AllowedCallsTotal,
		stats.BlockRate,
		stats.UniqueBlockedTools,
		stats.Uptime/3600,
	)

	if len(stats.TopBlockedTools) > 0 {
		output += "\n### Top Blocked Tools\n\n"
		for _, tool := range stats.TopBlockedTools {
			output += fmt.Sprintf("- `%s`: %d blocks\n", tool.Name, tool.Count)
		}
	}

	if len(stats.BlockedByReason) > 0 {
		output += "\n### Blocking Reasons\n\n"
		for reason, count := range stats.BlockedByReason {
			output += fmt.Sprintf("- %s: %d\n", reason, count)
		}
	}

	return output
}

// Helper methods

func (st *StatsTracker) copyMap(m map[string]int64) map[string]int64 {
	copy := make(map[string]int64)
	for k, v := range m {
		copy[k] = v
	}
	return copy
}

func (st *StatsTracker) topTools(tools map[string]int64, limit int) []ToolStat {
	// Convert to slice for sorting
	toolStats := make([]ToolStat, 0, len(tools))
	for name, count := range tools {
		toolStats = append(toolStats, ToolStat{Name: name, Count: count})
	}

	// Simple bubble sort (good enough for small lists)
	for i := 0; i < len(toolStats); i++ {
		for j := i + 1; j < len(toolStats); j++ {
			if toolStats[j].Count > toolStats[i].Count {
				toolStats[i], toolStats[j] = toolStats[j], toolStats[i]
			}
		}
	}

	if len(toolStats) > limit {
		return toolStats[:limit]
	}
	return toolStats
}

// StatsSnapshot represents a snapshot of current statistics.
type StatsSnapshot struct {
	Timestamp           int64             `json:"timestamp"`
	BlockedCallsTotal   int64             `json:"blocked_calls_total"`
	AllowedCallsTotal   int64             `json:"allowed_calls_total"`
	TotalCalls          int64             `json:"total_calls"`
	BlockRate           float64           `json:"block_rate"`
	UniqueBlockedTools  int               `json:"unique_blocked_tools"`
	UniqueAllowedTools  int               `json:"unique_allowed_tools"`
	BlockedByReason     map[string]int64  `json:"blocked_by_reason"`
	TopBlockedTools     []ToolStat        `json:"top_blocked_tools"`
	TopAllowedTools     []ToolStat        `json:"top_allowed_tools"`
	Uptime              float64           `json:"uptime_seconds"`
}

// ToolStat represents a statistic for a single tool.
type ToolStat struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// WeeklyStats aggregates statistics over a week.
type WeeklyStats struct {
	Period          string
	Days            []DailyStats
	TotalBlocked    int64
	TotalAllowed    int64
	TopBlockedTools []ToolStat
}
