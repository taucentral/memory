// smoke_test.go — end-to-end smoke tests (tasks 8.3 and 8.4).
//
// These tests drive the memory plugin through real components to
// exercise the full retrieve -> persist -> retrieve cycle and the
// project-scope isolation contract. The capturingClient replaces the
// SDK's NewFauxProvider here because NewFauxProvider ignores every
// response after the first (see pkg/tau/sdk.go:NewFauxProvider) —
// unsuitable for multi-turn scripting.
//
// These tests do NOT call t.Parallel: TestSmoke_ProjectScopeIsolation
// mutates the process-global cwd via os.Chdir, which would race with
// any other test in the same binary that reads cwd.
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tau "github.com/coevin/tau/pkg/tau"
)

// TestSmoke_MarkerRoundTrip drives the full persist -> retrieve cycle:
//
//  1. Turn 1 emits a marker-bracketed response. The observer extracts
//     the bracketed text and Puts it to the Store.
//  2. The test then verifies the Store holds the extracted entry with
//     the expected text AND the "memory" tag (the scope invariant).
//  3. Turn 2 emits a plain acknowledgment. The runtime dispatches the
//     retrieve mutator before calling the LLM, so the captured request
//     carries the memory block in req.System[0]. The test asserts the
//     block contains the persisted entry text.
//
// This replaces the prior overclaim that "verified the Store still
// had the entry after turn 2" without actually inspecting req.System.
func TestSmoke_MarkerRoundTrip(t *testing.T) {
	cwd := t.TempDir()
	storeDir := filepath.Join(cwd, ".tau", "memory")
	store, err := tau.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(cwd)
	})

	memPlugin := New(Config{
		Store:          store,
		Scope:          "global",
		ExtractionMode: "marker",
		IDMode:         "content_hash",
		TimeNow:        fixedTime,
	})

	// Capturing client: turn 1 carries the marker, turn 2 is plain.
	client := newCapturingClient(
		"the build uses pnpm <memory>build command is pnpm build</memory>",
		"acknowledged",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	var middleware []any
	middleware = append(middleware, memPlugin.Mutator(), memPlugin.Observer())

	sess, err := tau.CreateAgentSession(ctx, tau.Options{
		Cwd:        cwd,
		Model:      "faux",
		LLMClient:  client,
		Tools:      tau.BuiltinTools(),
		Settings:   tau.DefaultSettings(),
		Middleware: middleware,
	})
	if err != nil {
		t.Fatalf("CreateAgentSession: %v", err)
	}
	t.Cleanup(func() { _ = sess.Shutdown(ctx) })

	// Turn 1: emit the marker-bracketed response.
	if err := sess.Run(ctx, "remember this"); err != nil {
		t.Fatalf("Run turn 1: %v", err)
	}

	// Verify the Store now holds the extracted entry with text AND
	// the "memory" tag. The tag assertion is the part the prior test
	// omitted.
	entries, err := store.Query(ctx, tau.Query{
		TagsQuery: []string{"memory"},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Query after turn 1: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("store has %d entries after turn 1, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Text, "build command is pnpm build") {
		t.Errorf("entry text = %q, want substring 'build command is pnpm build'", entries[0].Text)
	}
	if !containsTag(entries[0].Tags, "memory") {
		t.Errorf("entry Tags = %v, want to contain 'memory'", entries[0].Tags)
	}

	// Turn 2: the retrieve mutator should prepend the entry to
	// req.System before the LLM call. The capturing client records
	// the request it receives, so req.System[0] tells us what the
	// mutator injected.
	if err := sess.Run(ctx, "anything"); err != nil {
		t.Fatalf("Run turn 2: %v", err)
	}

	captured := client.recordedRequests()
	if len(captured) < 2 {
		t.Fatalf("captured %d requests, want at least 2", len(captured))
	}
	second := captured[1]
	if len(second.System) == 0 {
		t.Fatalf("turn-2 req.System is empty; mutator did not inject memory")
	}
	tc, ok := second.System[0].(tau.TextContent)
	if !ok {
		t.Fatalf("turn-2 req.System[0] is %T, want TextContent", second.System[0])
	}
	if !strings.Contains(tc.Text, "build command is pnpm build") {
		t.Errorf("turn-2 req.System[0] = %q, want substring 'build command is pnpm build'", tc.Text)
	}
	if !strings.Contains(tc.Text, "Relevant memory entries:") {
		t.Errorf("turn-2 req.System[0] = %q, want the configured header", tc.Text)
	}

	// Store should still have exactly one entry (content_hash dedup;
	// turn 2's response carried no markers so nothing new was Put).
	entries2, err := store.Query(ctx, tau.Query{
		TagsQuery: []string{"memory"},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Query after turn 2: %v", err)
	}
	if len(entries2) != 1 {
		t.Errorf("store has %d entries after turn 2, want 1 (dedup)", len(entries2))
	}
}

// TestSmoke_ProjectScopeIsolation verifies that the project scope tag
// (derived from cwd) genuinely isolates two projects' memory pools
// when they share a single Store. The prior version of this test used
// two separate Stores plus the global scope, which trivially passed
// by relying on filesystem isolation rather than the plugin's tag
// semantics.
//
// Approach: save the process cwd, register cleanup to restore it,
// then os.Chdir between two temp directories. Under dirA, an observer
// writes a fact tagged with project:hashA; a mutator with the same
// scope retrieves it. Under dirB, a mutator with the same scope does
// NOT retrieve A's fact because its tag is project:hashB.
//
// Does NOT call t.Parallel: os.Chdir is process-global.
func TestSmoke_ProjectScopeIsolation(t *testing.T) {
	cwdA := t.TempDir()
	cwdB := t.TempDir()

	// Register the cwd restore LAST so it runs FIRST in LIFO order —
	// before either temp directory is removed by t.TempDir's cleanup.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// ONE shared store. The isolation mechanism under test is the tag,
	// not the filesystem.
	store := newMemStore()

	// --- Project A: cwdA -----------------------------------------
	if err := os.Chdir(cwdA); err != nil {
		t.Fatalf("chdir cwdA: %v", err)
	}
	tagsA := scopeTags(Config{Scope: "project"})
	if len(tagsA) != 2 || !strings.HasPrefix(tagsA[1], "project:") {
		t.Fatalf("tagsA = %v, want [memory, project:<8 hex>]", tagsA)
	}
	obsA := &memPersistObserver{cfg: Config{
		Store:                        store,
		Scope:                        "project",
		ExtractionMode:               "full_response",
		IDMode:                       "content_hash",
		UpdateTimestampOnContentHash: true,
		TimeNow:                      fixedTime,
	}}
	if err := obsA.ObserveResponse(context.Background(), nil, &tau.Response{
		Role:    tau.Role("assistant"),
		Content: []tau.ContentBlock{tau.TextContent{Text: "project A uses pnpm"}},
	}, nil); err != nil {
		t.Fatalf("obsA.ObserveResponse: %v", err)
	}
	// Sanity: A's mutator retrieves A's entry under A's tag.
	mutA := &memRetrieveMutator{cfg: Config{
		Store:          store,
		Scope:          "project",
		RetrieveLimit:  10,
		RetrieveHeader: "Memory:",
	}}
	reqA := &tau.Request{}
	if err := mutA.MutateRequest(context.Background(), reqA); err != nil {
		t.Fatalf("mutA.MutateRequest: %v", err)
	}
	if len(reqA.System) == 0 {
		t.Fatalf("project A did not retrieve its own entry")
	}
	tcA, _ := reqA.System[0].(tau.TextContent)
	if !strings.Contains(tcA.Text, "project A uses pnpm") {
		t.Errorf("project A's mutator did not retrieve its entry: %q", tcA.Text)
	}

	// --- Project B: cwdB -----------------------------------------
	if err := os.Chdir(cwdB); err != nil {
		t.Fatalf("chdir cwdB: %v", err)
	}
	tagsB := scopeTags(Config{Scope: "project"})
	if len(tagsB) != 2 || !strings.HasPrefix(tagsB[1], "project:") {
		t.Fatalf("tagsB = %v, want [memory, project:<8 hex>]", tagsB)
	}
	// Sanity: distinct cwds produce distinct project tags. This is
	// the structural invariant that makes tag-based isolation work.
	if tagsA[1] == tagsB[1] {
		t.Fatalf("project tags collided: tagsA=%v tagsB=%v (cwds %q vs %q)",
			tagsA, tagsB, cwdA, cwdB)
	}

	// Project B's mutator queries with tagsB, so it should not see
	// A's entry (which carries tagsA).
	mutB := &memRetrieveMutator{cfg: Config{
		Store:          store,
		Scope:          "project",
		RetrieveLimit:  10,
		RetrieveHeader: "Memory:",
	}}
	reqB := &tau.Request{}
	if err := mutB.MutateRequest(context.Background(), reqB); err != nil {
		t.Fatalf("mutB.MutateRequest: %v", err)
	}
	if len(reqB.System) > 0 {
		tcB, _ := reqB.System[0].(tau.TextContent)
		if strings.Contains(tcB.Text, "project A uses pnpm") {
			t.Errorf("project B retrieved project A's entry across scope tags: %q", tcB.Text)
		}
	}

	// Cross-check via the Store directly: tag-scoped queries are
	// disjoint across the two project tags.
	aEntries, err := store.Query(context.Background(), tau.Query{TagsQuery: tagsA, Limit: 100})
	if err != nil {
		t.Fatalf("Query tagsA: %v", err)
	}
	bEntries, err := store.Query(context.Background(), tau.Query{TagsQuery: tagsB, Limit: 100})
	if err != nil {
		t.Fatalf("Query tagsB: %v", err)
	}
	if len(aEntries) != 1 {
		t.Errorf("tagsA-scoped query returned %d entries, want 1", len(aEntries))
	}
	if len(bEntries) != 0 {
		t.Errorf("tagsB-scoped query returned %d entries, want 0 (B never wrote)", len(bEntries))
	}
}
