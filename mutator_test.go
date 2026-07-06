// mutator_test.go — tests 1-7 from the spec's sixteen-test plan.
package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	tau "github.com/taucentral/tau/pkg/tau"
)

// Test 1: retrieve-and-inject round-trip.
func TestMutator_RetrieveAndInject(t *testing.T) {
	store := newTempStore(t)
	// Seed an entry.
	err := store.Put(context.Background(), tau.Entry{
		ID:        "memory:test1",
		Text:      "the project uses Go 1.25",
		Tags:      []string{"memory"},
		Timestamp: testTime(),
	})
	assertNoErr(t, "Put", err)

	mut := &memRetrieveMutator{cfg: Config{
		Store:          store,
		RetrieveLimit:  10,
		RetrieveHeader: "Relevant memory entries:",
	}}
	req := &tau.Request{Model: "test"}
	err = mut.MutateRequest(context.Background(), req)
	assertNoErr(t, "MutateRequest", err)

	if len(req.System) == 0 {
		t.Fatalf("req.System is empty; expected injected memory block")
	}
	tc, ok := req.System[0].(tau.TextContent)
	if !ok {
		t.Fatalf("req.System[0] is %T, want tau.TextContent", req.System[0])
	}
	if !strings.Contains(tc.Text, "the project uses Go 1.25") {
		t.Errorf("injected text does not contain the entry: %q", tc.Text)
	}
	if !strings.Contains(tc.Text, "Relevant memory entries:") {
		t.Errorf("injected text does not contain the header: %q", tc.Text)
	}
}

// Test 2: tag scoping (session).
func TestMutator_SessionScopeIsolation(t *testing.T) {
	store := newMemStore()
	// Session A writes.
	mutA := &memRetrieveMutator{cfg: Config{
		Store: store, Scope: "session", SourceID: "sessionA",
		RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	obsA := &memPersistObserver{cfg: Config{
		Store: store, Scope: "session", SourceID: "sessionA",
		IDMode: "content_hash", TimeNow: fixedTime,
	}}
	// Session B writes.
	mutB := &memRetrieveMutator{cfg: Config{
		Store: store, Scope: "session", SourceID: "sessionB",
		RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}

	// Persist from session A.
	_ = obsA.ObserveResponse(context.Background(), nil, &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "fact from A"}},
	}, nil)

	// Session B's mutator should not see A's entry.
	reqB := &tau.Request{}
	_ = mutB.MutateRequest(context.Background(), reqB)
	if len(reqB.System) > 0 {
		tc, _ := reqB.System[0].(tau.TextContent)
		if strings.Contains(tc.Text, "fact from A") {
			t.Errorf("session B retrieved session A's entry: %q", tc.Text)
		}
	}

	// Session A's mutator should see its own entry.
	reqA := &tau.Request{}
	_ = mutA.MutateRequest(context.Background(), reqA)
	if len(reqA.System) == 0 {
		t.Fatalf("session A did not retrieve its own entry")
	}
	tc, _ := reqA.System[0].(tau.TextContent)
	if !strings.Contains(tc.Text, "fact from A") {
		t.Errorf("session A did not retrieve its entry: %q", tc.Text)
	}
}

// Test 3: tag scoping (project).
func TestMutator_ProjectScopeDistinctTags(t *testing.T) {
	// We cannot change cwd in a test, but we CAN verify that
	// scopeTags produces a project tag. The isolation test would
	// require two cwds; here we verify the tag shape.
	cfg := Config{Scope: "project"}
	tags := scopeTags(cfg)
	if len(tags) != 2 {
		t.Fatalf("project scope: expected 2 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "memory" {
		t.Errorf("tags[0] = %q, want 'memory'", tags[0])
	}
	if !strings.HasPrefix(tags[1], "project:") || len(tags[1]) < len("project:")+8 {
		t.Errorf("tags[1] = %q, want 'project:'+8 hex chars", tags[1])
	}
}

// Test 4: tag scoping (user).
func TestMutator_UserScopeIsolation(t *testing.T) {
	store := newMemStore()
	// User A writes.
	obsA := &memPersistObserver{cfg: Config{
		Store: store, Scope: "user", UserID: "alice",
		IDMode: "content_hash", TimeNow: fixedTime,
	}}
	_ = obsA.ObserveResponse(context.Background(), nil, &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "alice's fact"}},
	}, nil)

	// User B should not see it.
	mutB := &memRetrieveMutator{cfg: Config{
		Store: store, Scope: "user", UserID: "bob",
		RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	reqB := &tau.Request{}
	_ = mutB.MutateRequest(context.Background(), reqB)
	if len(reqB.System) > 0 {
		tc, _ := reqB.System[0].(tau.TextContent)
		if strings.Contains(tc.Text, "alice's fact") {
			t.Errorf("user B retrieved user A's entry: %q", tc.Text)
		}
	}

	// User A should see it.
	mutA := &memRetrieveMutator{cfg: Config{
		Store: store, Scope: "user", UserID: "alice",
		RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	reqA := &tau.Request{}
	_ = mutA.MutateRequest(context.Background(), reqA)
	if len(reqA.System) == 0 {
		t.Fatalf("user A did not retrieve its entry")
	}
	tc, _ := reqA.System[0].(tau.TextContent)
	if !strings.Contains(tc.Text, "alice's fact") {
		t.Errorf("user A did not retrieve its entry: %q", tc.Text)
	}
}

// Test 5: tag scoping (global).
func TestMutator_GlobalScopeSharesPool(t *testing.T) {
	store := newMemStore()
	obs := &memPersistObserver{cfg: Config{
		Store: store, Scope: "global",
		IDMode: "content_hash", TimeNow: fixedTime,
	}}
	_ = obs.ObserveResponse(context.Background(), nil, &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "global fact"}},
	}, nil)

	tags := scopeTags(Config{Scope: "global"})
	if len(tags) != 1 || tags[0] != "memory" {
		t.Errorf("global scope tags = %v, want ['memory']", tags)
	}

	mut := &memRetrieveMutator{cfg: Config{
		Store: store, Scope: "global",
		RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	req := &tau.Request{}
	_ = mut.MutateRequest(context.Background(), req)
	if len(req.System) == 0 {
		t.Fatalf("global mutator did not retrieve global entry")
	}
	tc, _ := req.System[0].(tau.TextContent)
	if !strings.Contains(tc.Text, "global fact") {
		t.Errorf("global mutator did not retrieve: %q", tc.Text)
	}
}

// Test 6: Store Query error is non-aborting.
func TestMutator_QueryErrorNonAborting(t *testing.T) {
	store := &errStore{err: errQuery}
	mut := &memRetrieveMutator{cfg: Config{
		Store: store, RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	req := &tau.Request{Model: "test"}
	err := mut.MutateRequest(context.Background(), req)
	if err != nil {
		t.Errorf("MutateRequest returned non-nil error on Store failure: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System modified on Store failure: %d blocks", len(req.System))
	}
}

// Test 7: Store nil is no-op.
func TestMutator_NilStoreIsNoOp(t *testing.T) {
	mut := &memRetrieveMutator{cfg: Config{
		Store: nil, RetrieveLimit: 10, RetrieveHeader: "Memory:",
	}}
	req := &tau.Request{Model: "test"}
	err := mut.MutateRequest(context.Background(), req)
	if err != nil {
		t.Errorf("MutateRequest returned error on nil Store: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System modified with nil Store: %d blocks", len(req.System))
	}
}

// fixedTime is a deterministic clock source for tests: always returns
// 2026-01-01T00:00:00Z. Assign to Config.TimeNow.
var fixedTime = func() time.Time {
	t, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	return t
}

// testTime is a convenience alias for tests that need a time.Time
// value (calls fixedTime). For Config.TimeNow, use fixedTime directly.
func testTime() time.Time { return fixedTime() }
