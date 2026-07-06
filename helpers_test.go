// helpers_test.go — shared test utilities for the memory plugin.
package memory

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	tau "github.com/taucentral/tau/pkg/tau"
)

// newTempStore returns a fresh FileStore in a temp directory, with
// t.Cleanup wired to close the store and remove the directory.
func newTempStore(t *testing.T) tau.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := tau.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore(%q): %v", dir, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(dir)
	})
	return store
}

// errStore is a tau.Store that returns err on every Query and Put.
type errStore struct {
	err error
}

func (s *errStore) Put(ctx context.Context, e tau.Entry) error {
	return s.err
}

func (s *errStore) Query(ctx context.Context, q tau.Query) ([]tau.Entry, error) {
	return nil, s.err
}

func (s *errStore) Close() error { return nil }

// memStore is a thread-safe in-memory Store for tests. It supports
// tag-based queries and tracks Put calls.
type memStore struct {
	mu      sync.Mutex
	entries map[string]tau.Entry
	putErr  error
	qryErr  error
}

func newMemStore() *memStore {
	return &memStore{entries: make(map[string]tau.Entry)}
}

func (s *memStore) Put(ctx context.Context, e tau.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	s.entries[e.ID] = e
	return nil
}

func (s *memStore) Query(ctx context.Context, q tau.Query) ([]tau.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.qryErr != nil {
		return nil, s.qryErr
	}
	var out []tau.Entry
	for _, e := range s.entries {
		// AND-match tags.
		match := true
		for _, want := range q.TagsQuery {
			found := false
			for _, have := range e.Tags {
				if have == want {
					found = true
					break
				}
			}
			if !found {
				match = false
				break
			}
		}
		if match {
			out = append(out, e)
		}
	}
	// Apply limit.
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (s *memStore) Close() error { return nil }

func (s *memStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// assertNoErr fails the test if err is non-nil.
func assertNoErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// containsTag reports whether want appears in tags. Used by observer
// tests that assert entries carry the "memory" tag plus the relevant
// scope tag per scopeTags(cfg).
func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// — capturing LLM client --------------------------------------------------
//
// Used by smoke_test.go to verify the runtime dispatches the retrieve
// mutator BEFORE handing the request to the provider. The mutator
// prepends memory entries to req.System; the captured Request will
// carry them in System[0], proving the end-to-end injection path.
//
// The SDK's NewFauxProvider ignores every response after the first
// (see pkg/tau/sdk.go:NewFauxProvider), so a dedicated client is
// required for multi-turn scripting.

// capturingClient is a deterministic LLMClient that plays back a list
// of canned responses (one per Stream call, in order) and records
// every Request it receives. Safe for concurrent use.
type capturingClient struct {
	mu        sync.Mutex
	responses []string
	calls     int
	recorded  []tau.Request
}

// newCapturingClient returns a client that will emit responses[0] on
// the first Stream call, responses[1] on the second, and so on. When
// the script is exhausted, subsequent calls emit "(script exhausted)".
func newCapturingClient(responses ...string) *capturingClient {
	return &capturingClient{responses: responses}
}

// Stream records req, emits the next scripted TextDelta + Final, and
// returns. The recorded request is retained for later inspection by
// recordedRequests.
func (c *capturingClient) Stream(ctx context.Context, req tau.Request) (<-chan tau.Delta, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.mu.Lock()
	c.recorded = append(c.recorded, req)
	var resp string
	if c.calls < len(c.responses) {
		resp = c.responses[c.calls]
	} else {
		resp = "(script exhausted)"
	}
	c.calls++
	c.mu.Unlock()

	ch := make(chan tau.Delta, 2)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			return
		case ch <- tau.TextDelta{Text: resp}:
		}
		select {
		case <-ctx.Done():
			return
		case ch <- tau.Final{StopReason: tau.StopReasonEndTurn}:
		}
	}()
	return ch, nil
}

// recordedRequests returns a copy of the requests seen so far.
func (c *capturingClient) recordedRequests() []tau.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]tau.Request, len(c.recorded))
	copy(out, c.recorded)
	return out
}

var _ tau.LLMClient = (*capturingClient)(nil)

// sentinel errors for fake-store injection.
var errQuery = errors.New("test: query failure")
var errPut = errors.New("test: put failure")
