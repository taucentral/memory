// concurrent_test.go — test 16: concurrent retrieve + persist.
package memory

import (
	"context"
	"sync"
	"testing"

	tau "github.com/taucentral/tau/pkg/tau"
)

// Test 16: concurrent retrieve + persist.
//
// Uses the thread-safe in-memory memStore rather than the FileStore.
// The FileStore's atomic-write pattern (temp file + rename) races when
// multiple goroutines Put the same content-hash ID concurrently — a
// known limitation of the file backend that is independent of the
// memory plugin's concurrency contract. The memStore exercises the
// plugin's interleaved retrieve/persist paths cleanly under -race.
func TestConcurrent_RetrieveAndPersist(t *testing.T) {
	store := newMemStore()
	cfg := Config{
		Store:                        store,
		Scope:                        "global",
		RetrieveLimit:                10,
		RetrieveHeader:               "Memory:",
		ExtractionMode:               "full_response",
		IDMode:                       "content_hash",
		UpdateTimestampOnContentHash: true, // default; exercise overwrite path
		TimeNow:                      fixedTime,
	}
	mut := &memRetrieveMutator{cfg: cfg}
	obs := &memPersistObserver{cfg: cfg}

	const goroutines = 8
	const iterations = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Half the goroutines persist; half retrieve.
	for i := 0; i < goroutines; i++ {
		if i%2 == 0 {
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					resp := &tau.Response{
						Role:    tau.Role("assistant"),
						Content: []tau.ContentBlock{tau.TextContent{Text: "concurrent fact"}},
					}
					_ = obs.ObserveResponse(context.Background(), nil, resp, nil)
				}
			}()
		} else {
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					req := &tau.Request{}
					_ = mut.MutateRequest(context.Background(), req)
				}
			}()
		}
	}
	wg.Wait()

	// After all writers finish, the store should have exactly one
	// entry (content_hash dedup — all writers wrote the same text).
	entries, err := store.Query(context.Background(), tau.Query{
		TagsQuery: []string{"memory"},
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("final Query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("store has %d entries after concurrent writes, want 1 (content_hash dedup)", len(entries))
	}
}
