package proxy

import (
	"fmt"
	"sync"
	"time"
)

type ResourceManager struct {
	mu              sync.RWMutex
	subscriptions   map[string]*Subscription
	cancellations   map[string]bool
	progressUpdates map[string]ProgressUpdate
	timeouts        map[string]*time.Timer
}

type Subscription struct {
	ID          string
	ResourceURI string
	ClientID    string
	CreatedAt   time.Time
	Active      bool
}

type ResourceListChangedNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		ResourceURI string `json:"resourceUri"`
	} `json:"params"`
}

type ResourceUpdatedNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		ResourceURI string `json:"resourceUri"`
		Contents    string `json:"contents"`
	} `json:"params"`
}

type CancelledNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		RequestID interface{} `json:"requestId"`
	} `json:"params"`
}

type ProgressNotification struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  ProgressUpdate `json:"params"`
}

type ProgressUpdate struct {
	RequestID interface{} `json:"requestId"`
	Progress  int64       `json:"progress"`
	Total     int64       `json:"total"`
}

func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		subscriptions:   make(map[string]*Subscription),
		cancellations:   make(map[string]bool),
		progressUpdates: make(map[string]ProgressUpdate),
		timeouts:        make(map[string]*time.Timer),
	}
}

func (rm *ResourceManager) Subscribe(subscriptionID, resourceURI, clientID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.subscriptions[subscriptionID]; exists {
		return fmt.Errorf("subscription already exists: %s", subscriptionID)
	}

	rm.subscriptions[subscriptionID] = &Subscription{
		ID:          subscriptionID,
		ResourceURI: resourceURI,
		ClientID:    clientID,
		CreatedAt:   time.Now(),
		Active:      true,
	}

	return nil
}

func (rm *ResourceManager) Unsubscribe(subscriptionID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	sub, exists := rm.subscriptions[subscriptionID]
	if !exists {
		return fmt.Errorf("subscription not found: %s", subscriptionID)
	}

	sub.Active = false
	delete(rm.subscriptions, subscriptionID)

	if timer, ok := rm.timeouts[subscriptionID]; ok {
		timer.Stop()
		delete(rm.timeouts, subscriptionID)
	}

	return nil
}

func (rm *ResourceManager) IsSubscribed(subscriptionID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	sub, exists := rm.subscriptions[subscriptionID]
	return exists && sub.Active
}

func (rm *ResourceManager) GetSubscription(subscriptionID string) (*Subscription, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	sub, exists := rm.subscriptions[subscriptionID]
	if !exists {
		return nil, fmt.Errorf("subscription not found: %s", subscriptionID)
	}

	return sub, nil
}

func (rm *ResourceManager) GetSubscriptionsByResource(resourceURI string) []*Subscription {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []*Subscription
	for _, sub := range rm.subscriptions {
		if sub.ResourceURI == resourceURI && sub.Active {
			result = append(result, sub)
		}
	}
	return result
}

func (rm *ResourceManager) NotifyListChanged(resourceURI string) ResourceListChangedNotification {
	return ResourceListChangedNotification{
		JSONRPC: "2.0",
		Method:  "resources/list_changed",
		Params: struct {
			ResourceURI string `json:"resourceUri"`
		}{
			ResourceURI: resourceURI,
		},
	}
}

func (rm *ResourceManager) NotifyUpdated(resourceURI, contents string) ResourceUpdatedNotification {
	return ResourceUpdatedNotification{
		JSONRPC: "2.0",
		Method:  "resources/updated",
		Params: struct {
			ResourceURI string `json:"resourceUri"`
			Contents    string `json:"contents"`
		}{
			ResourceURI: resourceURI,
			Contents:    contents,
		},
	}
}

func (rm *ResourceManager) CancelRequest(requestID interface{}) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.cancellations[fmt.Sprintf("%v", requestID)] = true

	if timer, ok := rm.timeouts[fmt.Sprintf("%v", requestID)]; ok {
		timer.Stop()
		delete(rm.timeouts, fmt.Sprintf("%v", requestID))
	}

	return nil
}

func (rm *ResourceManager) IsCancelled(requestID interface{}) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.cancellations[fmt.Sprintf("%v", requestID)]
}

func (rm *ResourceManager) NotifyCancelled(requestID interface{}) CancelledNotification {
	return CancelledNotification{
		JSONRPC: "2.0",
		Method:  "notifications/cancelled",
		Params: struct {
			RequestID interface{} `json:"requestId"`
		}{
			RequestID: requestID,
		},
	}
}

func (rm *ResourceManager) UpdateProgress(requestID interface{}, progress, total int64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if progress < 0 || total < 0 || progress > total {
		return fmt.Errorf("invalid progress values: progress=%d, total=%d", progress, total)
	}

	key := fmt.Sprintf("%v", requestID)
	rm.progressUpdates[key] = ProgressUpdate{
		RequestID: requestID,
		Progress:  progress,
		Total:     total,
	}

	return nil
}

func (rm *ResourceManager) GetProgress(requestID interface{}) (ProgressUpdate, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%v", requestID)
	update, exists := rm.progressUpdates[key]
	if !exists {
		return ProgressUpdate{}, fmt.Errorf("no progress update for request: %v", requestID)
	}

	return update, nil
}

func (rm *ResourceManager) NotifyProgress(requestID interface{}, progress, total int64) (ProgressNotification, error) {
	if err := rm.UpdateProgress(requestID, progress, total); err != nil {
		return ProgressNotification{}, err
	}

	return ProgressNotification{
		JSONRPC: "2.0",
		Method:  "notifications/progress",
		Params: ProgressUpdate{
			RequestID: requestID,
			Progress:  progress,
			Total:     total,
		},
	}, nil
}

func (rm *ResourceManager) SetTimeout(requestID interface{}, duration time.Duration) {
	rm.mu.Lock()
	key := fmt.Sprintf("%v", requestID)

	if oldTimer, exists := rm.timeouts[key]; exists {
		oldTimer.Stop()
	}

	timer := time.AfterFunc(duration, func() {
		rm.CancelRequest(requestID)
	})

	rm.timeouts[key] = timer
	rm.mu.Unlock()
}

func (rm *ResourceManager) ClearTimeout(requestID interface{}) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	key := fmt.Sprintf("%v", requestID)
	if timer, ok := rm.timeouts[key]; ok {
		timer.Stop()
		delete(rm.timeouts, key)
	}
}
