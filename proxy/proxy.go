package proxy

import (
	"database/sql"
	"fmt"
	"sync"
)

type Proxy struct {
	db                 *sql.DB
	mu                 sync.RWMutex
	serverCapabilities map[string]Capabilities
	clientCapabilities Capabilities
	intersectedCaps    Capabilities
	protocolVersion    string
	initialized        bool
	sessionID          string
}

func NewProxy(db *sql.DB) *Proxy {
	return &Proxy{
		db:                 db,
		serverCapabilities: make(map[string]Capabilities),
		protocolVersion:    MCPProtocolVersion,
	}
}

func (p *Proxy) Initialize(req InitRequest, serverResp InitResponseResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := ValidateProtocolVersion(req.Params.ProtocolVersion, serverResp.ProtocolVersion); err != nil {
		return err
	}

	p.clientCapabilities = req.Params.Capabilities
	p.serverCapabilities[serverResp.ServerInfo.Name] = serverResp.Capabilities
	p.intersectedCaps = IntersectCapabilities(req.Params.Capabilities, serverResp.Capabilities)
	p.protocolVersion = req.Params.ProtocolVersion
	p.initialized = true

	return nil
}

func (p *Proxy) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialized
}

func (p *Proxy) GetIntersectedCapabilities() Capabilities {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intersectedCaps
}

func (p *Proxy) GetProtocolVersion() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.protocolVersion
}

func (p *Proxy) CanSample() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intersectedCaps.Sampling != nil && p.intersectedCaps.Sampling.Tools
}

func (p *Proxy) CanElicit() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intersectedCaps.Elicitation != nil && p.intersectedCaps.Elicitation.Enabled
}

func (p *Proxy) CanSubscribe() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intersectedCaps.Subscribe
}

func (p *Proxy) HasListChanged() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intersectedCaps.ListChanged
}

func (p *Proxy) HasLogging() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ToBoolean(p.intersectedCaps.Logging)
}

func (p *Proxy) ValidateCapability(capability string) error {
	caps := p.GetIntersectedCapabilities()

	switch capability {
	case "sampling":
		if caps.Sampling == nil || !caps.Sampling.Tools {
			return fmt.Errorf("sampling capability not available")
		}
	case "sampling.tools":
		if caps.Sampling == nil || !caps.Sampling.Tools {
			return fmt.Errorf("sampling.tools capability not available")
		}
	case "elicitation":
		if caps.Elicitation == nil || !caps.Elicitation.Enabled {
			return fmt.Errorf("elicitation capability not available")
		}
	case "resources.subscribe":
		if !caps.Subscribe {
			return fmt.Errorf("resources.subscribe capability not available")
		}
	case "resources.listChanged":
		if !caps.ListChanged {
			return fmt.Errorf("resources.listChanged capability not available")
		}
	case "logging":
		if !ToBoolean(caps.Logging) {
			return fmt.Errorf("logging capability not available")
		}
	}

	return nil
}

func (p *Proxy) SetSessionID(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionID = sessionID
}

func (p *Proxy) GetSessionID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sessionID
}

// ToBoolean converts an interface{} to bool, handling both bool and object types
func ToBoolean(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case map[string]interface{}:
		// If it's an object, treat it as truthy
		return len(val) > 0
	default:
		return false
	}
}
