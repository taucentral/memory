// default_config_test.go — test 17: default config values.
package memory

import (
	"context"
	"testing"
	"time"

	tau "github.com/coevin/tau/pkg/tau"
)

// Test 17: default config populates expected values.
func TestDefaultConfig(t *testing.T) {
	store := newTempStore(t)
	// Zero-value Config with just Store set.
	cfg := Config{Store: store}
	plugin := New(cfg)

	// Verify defaults were applied.
	actual := plugin.cfg
	if actual.RetrieveLimit != 10 {
		t.Errorf("RetrieveLimit = %d, want 10", actual.RetrieveLimit)
	}
	if actual.RetrieveHeader != "Relevant memory entries:" {
		t.Errorf("RetrieveHeader = %q, want 'Relevant memory entries:'", actual.RetrieveHeader)
	}
	if actual.ExtractionMode != "full_response" {
		t.Errorf("ExtractionMode = %q, want 'full_response'", actual.ExtractionMode)
	}
	if actual.IDMode != "content_hash" {
		t.Errorf("IDMode = %q, want 'content_hash'", actual.IDMode)
	}
	if actual.MarkerOpen != "<memory>" {
		t.Errorf("MarkerOpen = %q, want '<memory>'", actual.MarkerOpen)
	}
	if actual.MarkerClose != "</memory>" {
		t.Errorf("MarkerClose = %q, want '</memory>'", actual.MarkerClose)
	}
	if !actual.UpdateTimestampOnContentHash {
		t.Errorf("UpdateTimestampOnContentHash = false, want true")
	}
	if actual.TimeNow == nil {
		t.Errorf("TimeNow is nil, want non-nil")
	}

	// Mutator and observer should work without panics.
	if plugin.Mutator() == nil {
		t.Errorf("Mutator() returned nil")
	}
	if plugin.Observer() == nil {
		t.Errorf("Observer() returned nil")
	}

	// Exercise the mutator with an empty store (no-op).
	req := &tau.Request{}
	if err := plugin.Mutator().MutateRequest(context.Background(), req); err != nil {
		t.Errorf("MutateRequest with defaults: %v", err)
	}

	// Exercise the observer with a simple response.
	resp := &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "default test"}},
	}
	if err := plugin.Observer().ObserveResponse(context.Background(), nil, resp, nil); err != nil {
		t.Errorf("ObserveResponse with defaults: %v", err)
	}
}

// TestDefaultConfig_TimeNowNotNil verifies the TimeNow default is
// callable and returns a reasonable time.
func TestDefaultConfig_TimeNowNotNil(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.TimeNow == nil {
		t.Fatalf("TimeNow nil after applyDefaults")
	}
	now := cfg.TimeNow()
	if now.IsZero() {
		t.Errorf("TimeNow() returned zero time")
	}
	// Should be close to time.Now() (within a minute).
	if diff := time.Since(now); diff > time.Minute || diff < -time.Minute {
		t.Errorf("TimeNow() diff = %v, want within 1 minute of now", diff)
	}
}
