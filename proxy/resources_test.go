package proxy

import (
	"testing"
	"time"
)

func TestResourceSubscription(t *testing.T) {
	rm := NewResourceManager()

	err := rm.Subscribe("sub-1", "file:///example.txt", "client-1")
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	if !rm.IsSubscribed("sub-1") {
		t.Errorf("expected subscription to be active")
	}

	sub, err := rm.GetSubscription("sub-1")
	if err != nil {
		t.Fatalf("failed to get subscription: %v", err)
	}

	if sub.ResourceURI != "file:///example.txt" {
		t.Errorf("expected resource URI file:///example.txt, got %s", sub.ResourceURI)
	}
}

func TestResourceSubscriptionDuplicate(t *testing.T) {
	rm := NewResourceManager()

	rm.Subscribe("sub-1", "file:///example.txt", "client-1")

	err := rm.Subscribe("sub-1", "file:///other.txt", "client-1")
	if err == nil {
		t.Errorf("expected error for duplicate subscription")
	}
}

func TestResourceUnsubscription(t *testing.T) {
	rm := NewResourceManager()

	rm.Subscribe("sub-1", "file:///example.txt", "client-1")

	if !rm.IsSubscribed("sub-1") {
		t.Fatalf("expected subscription to be active before unsubscribe")
	}

	err := rm.Unsubscribe("sub-1")
	if err != nil {
		t.Fatalf("failed to unsubscribe: %v", err)
	}

	if rm.IsSubscribed("sub-1") {
		t.Errorf("expected subscription to be inactive after unsubscribe")
	}
}

func TestResourceListChangedNotification(t *testing.T) {
	rm := NewResourceManager()

	notif := rm.NotifyListChanged("file:///example.txt")

	if notif.Method != "resources/list_changed" {
		t.Errorf("expected method resources/list_changed, got %s", notif.Method)
	}

	if notif.Params.ResourceURI != "file:///example.txt" {
		t.Errorf("expected resource URI file:///example.txt, got %s", notif.Params.ResourceURI)
	}
}

func TestResourceUpdatedNotification(t *testing.T) {
	rm := NewResourceManager()

	notif := rm.NotifyUpdated("file:///example.txt", "new content")

	if notif.Method != "resources/updated" {
		t.Errorf("expected method resources/updated, got %s", notif.Method)
	}

	if notif.Params.Contents != "new content" {
		t.Errorf("expected content 'new content', got %s", notif.Params.Contents)
	}
}

func TestRequestCancellation(t *testing.T) {
	rm := NewResourceManager()

	if rm.IsCancelled(1) {
		t.Errorf("expected request not to be cancelled initially")
	}

	rm.CancelRequest(1)

	if !rm.IsCancelled(1) {
		t.Errorf("expected request to be cancelled")
	}
}

func TestCancelledNotification(t *testing.T) {
	rm := NewResourceManager()

	notif := rm.NotifyCancelled(1)

	if notif.Method != "notifications/cancelled" {
		t.Errorf("expected method notifications/cancelled, got %s", notif.Method)
	}

	if notif.Params.RequestID != 1 {
		t.Errorf("expected request ID 1, got %v", notif.Params.RequestID)
	}
}

func TestProgressUpdate(t *testing.T) {
	rm := NewResourceManager()

	err := rm.UpdateProgress(1, 50, 100)
	if err != nil {
		t.Fatalf("failed to update progress: %v", err)
	}

	progress, err := rm.GetProgress(1)
	if err != nil {
		t.Fatalf("failed to get progress: %v", err)
	}

	if progress.Progress != 50 || progress.Total != 100 {
		t.Errorf("expected progress 50/100, got %d/%d", progress.Progress, progress.Total)
	}
}

func TestProgressValidation(t *testing.T) {
	tests := []struct {
		name      string
		progress  int64
		total     int64
		shouldErr bool
	}{
		{"valid", 50, 100, false},
		{"progress > total", 150, 100, true},
		{"negative progress", -1, 100, true},
		{"negative total", 50, -1, true},
		{"zero progress", 0, 100, false},
		{"complete", 100, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := NewResourceManager()
			err := rm.UpdateProgress(1, tt.progress, tt.total)
			if (err != nil) != tt.shouldErr {
				t.Errorf("update progress failed=%v, expected failed=%v: %v", err != nil, tt.shouldErr, err)
			}
		})
	}
}

func TestProgressNotification(t *testing.T) {
	rm := NewResourceManager()

	notif, err := rm.NotifyProgress(1, 50, 100)
	if err != nil {
		t.Fatalf("failed to notify progress: %v", err)
	}

	if notif.Method != "notifications/progress" {
		t.Errorf("expected method notifications/progress, got %s", notif.Method)
	}

	if notif.Params.Progress != 50 || notif.Params.Total != 100 {
		t.Errorf("expected progress 50/100, got %d/%d", notif.Params.Progress, notif.Params.Total)
	}
}

func TestRequestTimeout(t *testing.T) {
	rm := NewResourceManager()

	rm.SetTimeout(1, 100*time.Millisecond)

	if rm.IsCancelled(1) {
		t.Errorf("expected request not to be cancelled initially")
	}

	time.Sleep(150 * time.Millisecond)

	if !rm.IsCancelled(1) {
		t.Errorf("expected request to be auto-cancelled after timeout")
	}
}

func TestClearTimeout(t *testing.T) {
	rm := NewResourceManager()

	rm.SetTimeout(1, 100*time.Millisecond)
	rm.ClearTimeout(1)

	time.Sleep(150 * time.Millisecond)

	if rm.IsCancelled(1) {
		t.Errorf("expected request not to be cancelled after clearing timeout")
	}
}

func TestGetSubscriptionsByResource(t *testing.T) {
	rm := NewResourceManager()

	rm.Subscribe("sub-1", "file:///example.txt", "client-1")
	rm.Subscribe("sub-2", "file:///example.txt", "client-2")
	rm.Subscribe("sub-3", "file:///other.txt", "client-3")

	subs := rm.GetSubscriptionsByResource("file:///example.txt")

	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions for file:///example.txt, got %d", len(subs))
	}

	for _, sub := range subs {
		if sub.ResourceURI != "file:///example.txt" {
			t.Errorf("expected resource URI file:///example.txt, got %s", sub.ResourceURI)
		}
	}
}

func TestSubscriptionInactiveAfterUnsubscribe(t *testing.T) {
	rm := NewResourceManager()

	rm.Subscribe("sub-1", "file:///example.txt", "client-1")
	rm.Unsubscribe("sub-1")

	subs := rm.GetSubscriptionsByResource("file:///example.txt")

	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions after unsubscribe, got %d", len(subs))
	}
}
