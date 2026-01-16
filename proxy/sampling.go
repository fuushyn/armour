package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
)

type SamplingGuard struct {
	allowedServers      map[string]bool
	mu                  sync.RWMutex
	disabledOnTransport map[string]bool
}

func NewSamplingGuard() *SamplingGuard {
	return &SamplingGuard{
		allowedServers:      make(map[string]bool),
		disabledOnTransport: make(map[string]bool),
	}
}

func (sg *SamplingGuard) AllowServer(serverID string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.allowedServers[serverID] = true
}

func (sg *SamplingGuard) DenyServer(serverID string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	delete(sg.allowedServers, serverID)
}

func (sg *SamplingGuard) IsServerAllowed(serverID string) bool {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return sg.allowedServers[serverID]
}

func (sg *SamplingGuard) DisableOnTransport(transportType string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.disabledOnTransport[transportType] = true
}

func (sg *SamplingGuard) IsDisabledOnTransport(transportType string) bool {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return sg.disabledOnTransport[transportType]
}

type SamplingRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      interface{}    `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  SamplingParams `json:"params"`
}

type SamplingParams struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (sg *SamplingGuard) ValidateToolUseAndResultBalance(messages []Message) error {
	toolUseIDs := make(map[string]bool)

	for i, msg := range messages {
		if msg.Role == "assistant" {
			blocks, err := parseContentBlocks(msg.Content)
			if err != nil {
				return fmt.Errorf("message %d: failed to parse assistant content: %v", i, err)
			}

			for _, block := range blocks {
				if block.Type == "tool_use" {
					if block.ToolUseBlock != nil {
						toolUseIDs[block.ToolUseBlock.ID] = true
					}
				}
			}
		} else if msg.Role == "user" {
			blocks, err := parseContentBlocks(msg.Content)
			if err != nil {
				return fmt.Errorf("message %d: failed to parse user content: %v", i, err)
			}

			hasToolResult := false
			hasOtherContent := false

			for _, block := range blocks {
				if block.Type == "tool_result" {
					hasToolResult = true
					if block.ToolResultBlock != nil {
						if !toolUseIDs[block.ToolResultBlock.ToolUseID] {
							return fmt.Errorf("message %d: tool_result references unknown tool_use_id: %s",
								i, block.ToolResultBlock.ToolUseID)
						}
					}
				} else if block.Type == "text" {
					hasOtherContent = true
				}
			}

			if hasToolResult && hasOtherContent {
				return fmt.Errorf("message %d: cannot mix tool_result with other content", i)
			}
		}
	}

	return nil
}

type ContentBlock struct {
	Type            string
	ToolUseBlock    *ToolUseBlock
	ToolResultBlock *ToolResultBlock
	TextBlock       *TextBlock
}

func parseContentBlocks(content json.RawMessage) ([]ContentBlock, error) {
	var blocks interface{}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, err
	}

	var result []ContentBlock

	switch v := blocks.(type) {
	case []interface{}:
		for _, item := range v {
			if blockMap, ok := item.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)

				switch blockType {
				case "tool_use":
					tub := &ToolUseBlock{}
					data, _ := json.Marshal(blockMap)
					if err := json.Unmarshal(data, tub); err == nil {
						result = append(result, ContentBlock{
							Type:         "tool_use",
							ToolUseBlock: tub,
						})
					}
				case "tool_result":
					trb := &ToolResultBlock{}
					data, _ := json.Marshal(blockMap)
					if err := json.Unmarshal(data, trb); err == nil {
						result = append(result, ContentBlock{
							Type:            "tool_result",
							ToolResultBlock: trb,
						})
					}
				case "text":
					tb := &TextBlock{}
					data, _ := json.Marshal(blockMap)
					if err := json.Unmarshal(data, tb); err == nil {
						result = append(result, ContentBlock{
							Type:      "text",
							TextBlock: tb,
						})
					}
				}
			}
		}
	case map[string]interface{}:
		blockType, _ := v["type"].(string)
		switch blockType {
		case "tool_use":
			tub := &ToolUseBlock{}
			data, _ := json.Marshal(v)
			if err := json.Unmarshal(data, tub); err == nil {
				result = append(result, ContentBlock{
					Type:         "tool_use",
					ToolUseBlock: tub,
				})
			}
		case "tool_result":
			trb := &ToolResultBlock{}
			data, _ := json.Marshal(v)
			if err := json.Unmarshal(data, trb); err == nil {
				result = append(result, ContentBlock{
					Type:            "tool_result",
					ToolResultBlock: trb,
				})
			}
		case "text":
			tb := &TextBlock{}
			data, _ := json.Marshal(v)
			if err := json.Unmarshal(data, tb); err == nil {
				result = append(result, ContentBlock{
					Type:      "text",
					TextBlock: tb,
				})
			}
		}
	}

	return result, nil
}

func (sg *SamplingGuard) ValidateSamplingRequest(req SamplingRequest, proxy *Proxy, transportType string) error {
	if !proxy.CanSample() {
		return fmt.Errorf("sampling capability not available")
	}

	if sg.IsDisabledOnTransport(transportType) {
		return fmt.Errorf("sampling disabled on %s transport", transportType)
	}

	return sg.ValidateToolUseAndResultBalance(req.Params.Messages)
}

func isValidToolUseID(id string) bool {
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	return pattern.MatchString(id)
}
