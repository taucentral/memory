// observer_test.go — tests 8-15 from the spec's sixteen-test plan.
package memory

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tau "github.com/taucentral/tau/pkg/tau"
)

// counterStore tracks Put calls. It's a memStore with a counter.
type counterStore struct {
	*memStore
	puts int32
}

func newCounterStore() *counterStore {
	return &counterStore{memStore: newMemStore()}
}

func (s *counterStore) Put(ctx context.Context, e tau.Entry) error {
	atomic.AddInt32(&s.puts, 1)
	return s.memStore.Put(ctx, e)
}

// putCount returns the number of Put calls recorded by s.
func putCount(s *counterStore) int32 { return atomic.LoadInt32(&s.puts) }

// Test 8: marker extraction.
func TestObserver_MarkerExtraction(t *testing.T) {
	store := newCounterStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "marker",
		MarkerOpen:     "<memory>",
		MarkerClose:    "</memory>",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
	}}
	resp := &tau.Response{
		Role: tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: `intro <memory>fact A</memory> some text <memory>fact B</memory> outro`}},
	}
	err := obs.ObserveResponse(context.Background(), nil, resp, nil)
	assertNoErr(t, "ObserveResponse", err)

	if got := putCount(store); got != 2 {
		t.Errorf("Put called %d times, want 2", got)
	}
	entries, err := store.Query(context.Background(), tau.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("store has %d entries, want 2", len(entries))
	}
	// Verify both facts are in the store and carry the "memory" tag.
	var texts []string
	for _, e := range entries {
		texts = append(texts, e.Text)
		if !containsTag(e.Tags, "memory") {
			t.Errorf("entry %q missing 'memory' tag; got %v", e.Text, e.Tags)
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "fact A") || !strings.Contains(joined, "fact B") {
		t.Errorf("store missing expected facts: %v", texts)
	}
}

// Test 9: full-response extraction.
func TestObserver_FullResponseExtraction(t *testing.T) {
	store := newCounterStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "full_response",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
	}}
	resp := &tau.Response{
		Role: tau.Role("assistant"),
		Content: []tau.ContentBlock{
			tau.TextContent{Text: "part one. "},
			tau.TextContent{Text: "part two."},
		},
	}
	err := obs.ObserveResponse(context.Background(), nil, resp, nil)
	assertNoErr(t, "ObserveResponse", err)

	if got := putCount(store); got != 1 {
		t.Errorf("Put called %d times, want 1", got)
	}
	entries, err := store.Query(context.Background(), tau.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("store has %d entries, want 1", len(entries))
	}
	if entries[0].Text != "part one. part two." {
		t.Errorf("entry text = %q, want concatenated full text", entries[0].Text)
	}
	if !containsTag(entries[0].Tags, "memory") {
		t.Errorf("entry missing 'memory' tag; got %v", entries[0].Tags)
	}
}

// Test 10: content-hash ID mode (dedup).
func TestObserver_ContentHashDedup(t *testing.T) {
	store := newCounterStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode:               "full_response",
		IDMode:                       "content_hash",
		UpdateTimestampOnContentHash: true, // default; both Puts proceed, store dedups
		TimeNow:                      fixedTime,
	}}
	resp := &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "identical fact"}},
	}
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)

	if got := putCount(store); got != 2 {
		t.Errorf("Put called %d times, want 2 (both attempts call Put)", got)
	}
	if store.count() != 1 {
		t.Errorf("store has %d entries, want 1 (deduped by content_hash ID)", store.count())
	}
}

// TestObserver_UpdateTimestampOnContentHashPreserves verifies the
// UpdateTimestampOnContentHash=false branch. When two responses carry
// identical extracted text under IDMode=content_hash, the SECOND Put
// must be skipped so the original entry — timestamp and all — is
// preserved verbatim. The default (true) path overwrites, refreshing
// the timestamp; the opt-out preserves the first write.
func TestObserver_UpdateTimestampOnContentHashPreserves(t *testing.T) {
	t.Run("false preserves first timestamp", func(t *testing.T) {
		store := newCounterStore()
		// Counter-based clock so each Put attempt would carry a
		// distinct timestamp if it proceeded.
		var ts int64
		clock := func() time.Time {
			n := atomic.AddInt64(&ts, 1)
			return time.Unix(n, 0).UTC()
		}
		obs := &memPersistObserver{cfg: Config{
			Store: store, Scope: "global",
			ExtractionMode:               "full_response",
			IDMode:                       "content_hash",
			UpdateTimestampOnContentHash: false,
			TimeNow:                      clock,
		}}
		resp := &tau.Response{
			Role:    tau.Role("assistant"),
			Content: []tau.ContentBlock{tau.TextContent{Text: "identical fact"}},
		}
		assertNoErr(t, "ObserveResponse 1", obs.ObserveResponse(context.Background(), nil, resp, nil))
		assertNoErr(t, "ObserveResponse 2", obs.ObserveResponse(context.Background(), nil, resp, nil))

		// The second Put must have been skipped: only one Put call,
		// not two. (shouldPreserveExisting intercepted the second.)
		if got := putCount(store); got != 1 {
			t.Errorf("Put called %d times, want 1 (second Put should be skipped)", got)
		}
		entries, err := store.Query(context.Background(), tau.Query{})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("store has %d entries, want 1", len(entries))
		}
		// The preserved timestamp is the FIRST clock value (1), not
		// the second (2). If preservation failed, the entry would
		// carry timestamp 2 from the overwrite.
		want := time.Unix(1, 0).UTC()
		if !entries[0].Timestamp.Equal(want) {
			t.Errorf("entry timestamp = %v, want %v (preserved first write)",
				entries[0].Timestamp, want)
		}
	})

	t.Run("true overwrites and refreshes timestamp (default)", func(t *testing.T) {
		store := newCounterStore()
		var ts int64
		clock := func() time.Time {
			n := atomic.AddInt64(&ts, 1)
			return time.Unix(n, 0).UTC()
		}
		obs := &memPersistObserver{cfg: Config{
			Store: store, Scope: "global",
			ExtractionMode:               "full_response",
			IDMode:                       "content_hash",
			UpdateTimestampOnContentHash: true, // default
			TimeNow:                      clock,
		}}
		resp := &tau.Response{
			Role:    tau.Role("assistant"),
			Content: []tau.ContentBlock{tau.TextContent{Text: "identical fact"}},
		}
		assertNoErr(t, "ObserveResponse 1", obs.ObserveResponse(context.Background(), nil, resp, nil))
		assertNoErr(t, "ObserveResponse 2", obs.ObserveResponse(context.Background(), nil, resp, nil))

		// Both Puts proceeded; the entry now carries the SECOND timestamp.
		if got := putCount(store); got != 2 {
			t.Errorf("Put called %d times, want 2 (both should proceed)", got)
		}
		entries, err := store.Query(context.Background(), tau.Query{})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("store has %d entries, want 1", len(entries))
		}
		want := time.Unix(2, 0).UTC()
		if !entries[0].Timestamp.Equal(want) {
			t.Errorf("entry timestamp = %v, want %v (refreshed by second Put)",
				entries[0].Timestamp, want)
		}
	})

	t.Run("timestamp ID mode ignores preservation flag", func(t *testing.T) {
		// In timestamp ID mode, every Put produces a distinct ID, so
		// shouldPreserveExisting returns false regardless of the
		// UpdateTimestampOnContentHash setting. Both Puts proceed.
		store := newCounterStore()
		var ts int64
		clock := func() time.Time {
			n := atomic.AddInt64(&ts, 1)
			return time.Unix(n, 0).UTC()
		}
		obs := &memPersistObserver{cfg: Config{
			Store: store, Scope: "global",
			ExtractionMode:               "full_response",
			IDMode:                       "timestamp",
			UpdateTimestampOnContentHash: false, // should be ignored
			TimeNow:                      clock,
		}}
		resp := &tau.Response{
			Role:    tau.Role("assistant"),
			Content: []tau.ContentBlock{tau.TextContent{Text: "identical fact"}},
		}
		assertNoErr(t, "ObserveResponse 1", obs.ObserveResponse(context.Background(), nil, resp, nil))
		assertNoErr(t, "ObserveResponse 2", obs.ObserveResponse(context.Background(), nil, resp, nil))

		if got := putCount(store); got != 2 {
			t.Errorf("Put called %d times, want 2 (timestamp mode does not preserve)", got)
		}
		if store.count() != 2 {
			t.Errorf("store has %d entries, want 2 (timestamp mode produces distinct IDs)", store.count())
		}
	})
}

// Test 11: timestamp ID mode (no dedup).
func TestObserver_TimestampNoDedup(t *testing.T) {
	store := newCounterStore()
	// Use a counter-based clock so two calls produce different timestamps.
	var ts int64
	clock := func() time.Time {
		n := atomic.AddInt64(&ts, 1)
		return time.Unix(n, 0).UTC()
	}
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "full_response",
		IDMode:         "timestamp",
		TimeNow:        clock,
	}}
	resp := &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "repeated fact"}},
	}
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)

	if store.count() != 2 {
		t.Errorf("store has %d entries, want 2 (timestamp mode does not dedup)", store.count())
	}
}

// Test 12: redactor false return skips entry.
func TestObserver_RedactorSkip(t *testing.T) {
	store := newCounterStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "marker",
		MarkerOpen:     "<memory>", MarkerClose: "</memory>",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
		Redactor: func(text string) (string, bool) {
			if strings.Contains(text, "skip") {
				return "", false
			}
			return text, true
		},
	}}
	resp := &tau.Response{
		Role: tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: `<memory>keep me</memory> <memory>skip me</memory>`}},
	}
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)

	if got := putCount(store); got != 1 {
		t.Errorf("Put called %d times, want 1 (skip should suppress one)", got)
	}
	for _, e := range store.memStore.entries {
		if e.Text != "keep me" {
			t.Errorf("unexpected entry text: %q", e.Text)
		}
	}
}

// Test 13: redactor mutates text.
func TestObserver_RedactorMutate(t *testing.T) {
	store := newCounterStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "full_response",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
		Redactor: func(text string) (string, bool) {
			return strings.ToUpper(text), true
		},
	}}
	resp := &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "lowercase fact"}},
	}
	_ = obs.ObserveResponse(context.Background(), nil, resp, nil)

	if store.count() != 1 {
		t.Fatalf("store has %d entries, want 1", store.count())
	}
	for _, e := range store.memStore.entries {
		if e.Text != "LOWERCASE FACT" {
			t.Errorf("entry text = %q, want uppercased", e.Text)
		}
	}
}

// Test 14: Store Put error is non-aborting.
func TestObserver_PutErrorNonAborting(t *testing.T) {
	store := newMemStore()
	store.putErr = errPut
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "full_response",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
	}}
	resp := &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "some fact"}},
	}
	// Should not return an error (non-aborting).
	err := obs.ObserveResponse(context.Background(), nil, resp, nil)
	if err != nil {
		t.Errorf("ObserveResponse returned error on Store Put failure: %v", err)
	}
}

// Test 15: panic recovery (Store Put panic is recovered by safePut).
//
// Note: the extractor (extractCandidates) uses comma-ok type assertion
// throughout and cannot panic on a "malformed" Content block — any
// non-TextContent block is simply skipped. The reachable panic
// boundary in the observer is safePut, exercised here via a Store
// whose Put panics (mirroring the spec's "buggy backend" scenario).
func TestObserver_PanicRecovery(t *testing.T) {
	store := &panicStore{}
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		ExtractionMode: "full_response",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
	}}
	resp := &tau.Response{
		Role: tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{
			Text: "fact that triggers a panicking Put",
		}},
	}
	// Should not crash; safePut should recover.
	err := obs.ObserveResponse(context.Background(), nil, resp, nil)
	if err != nil {
		t.Errorf("ObserveResponse returned error after panic: %v", err)
	}
}

// panicStore is a tau.Store whose Put always panics.
type panicStore struct{}

func (s *panicStore) Put(ctx context.Context, e tau.Entry) error {
	panic("test: store Put panic")
}
func (s *panicStore) Query(ctx context.Context, q tau.Query) ([]tau.Entry, error) {
	return nil, nil
}
func (s *panicStore) Close() error { return nil }
