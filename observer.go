// observer.go — the persist observer (ResponseObserver).
//
// Extracts memory candidates from each assistant response, applies
// the optional Redactor, and writes them to the Store. Errors and
// panics are recovered per the asymmetric contract: a buggy Store
// backend or malformed Content block must not crash the runtime.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"

	tau "github.com/coevin/tau/pkg/tau"
)

// memPersistObserver extracts memory candidates from responses and
// writes them to the Store.
type memPersistObserver struct {
	cfg Config
}

// ObserveResponse implements tau.ResponseObserver. It extracts
// candidates, applies the redactor, and Puts each to the Store.
//
// All errors are recovered and logged; the observer always returns
// nil (non-aborting per the asymmetric contract).
func (o *memPersistObserver) ObserveResponse(ctx context.Context, req *tau.Request, resp *tau.Response, streamErr error) error {
	_ = req // unused; memory extraction is response-only
	_ = streamErr
	if o.cfg.Store == nil {
		return nil
	}
	if resp == nil {
		return nil
	}
	candidates := o.safeExtract(resp)
	for _, text := range candidates {
		if o.cfg.Redactor != nil {
			redacted, ok := o.cfg.Redactor(text)
			if !ok {
				continue
			}
			text = redacted
		}
		id := o.entryID(text)
		if o.shouldPreserveExisting(ctx, id) {
			// IDMode == "content_hash" && !UpdateTimestampOnContentHash
			// && an entry with this ID already exists. Skip the Put so
			// the original entry (timestamp included) is preserved.
			continue
		}
		entry := tau.Entry{
			ID:        id,
			Text:      text,
			Tags:      scopeTags(o.cfg),
			Timestamp: o.cfg.TimeNow(),
			Source:    o.cfg.SourceID,
		}
		_ = o.safePut(ctx, entry)
	}
	return nil
}

// preserveCheckLimit caps the Query issued by shouldPreserveExisting.
// It is intentionally large: the lookup needs to find a single entry
// by ID among all entries sharing the plugin's scope tags, and a low
// cap would falsely report "no collision" on stores with many entries.
const preserveCheckLimit = 1000

// shouldPreserveExisting reports whether an existing entry with the
// given ID should be preserved verbatim (timestamp included) rather
// than overwritten by a new Put. Returns true only when ALL of:
//
//   - IDMode is "content_hash" (other modes produce distinct IDs and
//     never collide).
//   - UpdateTimestampOnContentHash is false (the opt-in preservation
//     knob).
//   - the Store already holds an entry with the same ID under the
//     plugin's scope tags.
//
// Any Query error or panic returns false so the Put proceeds — the
// asymmetric contract prefers "refresh the timestamp" over "abort the
// turn" when the preservation check cannot be answered reliably.
func (o *memPersistObserver) shouldPreserveExisting(ctx context.Context, id string) (preserves bool) {
	if o.cfg.IDMode != "content_hash" || o.cfg.UpdateTimestampOnContentHash {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("memory: preserve-check panic: %v", r)
			preserves = false
		}
	}()
	existing, err := o.cfg.Store.Query(ctx, tau.Query{
		TagsQuery: scopeTags(o.cfg),
		Limit:     preserveCheckLimit,
	})
	if err != nil {
		log.Printf("memory: preserve-check query: %v", err)
		return false
	}
	for _, e := range existing {
		if e.ID == id {
			return true
		}
	}
	return false
}

// entryID generates the Store entry ID per the configured IDMode.
//
//   - "content_hash" (default): "memory:" + sha256(text)[:16].
//     Identical text produces the same ID so Store.Put deduplicates.
//   - "timestamp" / "": "memory:" + UTC timestamp.
//     Each occurrence is a distinct entry; no dedup.
func (o *memPersistObserver) entryID(text string) string {
	switch o.cfg.IDMode {
	case "content_hash":
		sum := sha256.Sum256([]byte(text))
		return "memory:" + hex.EncodeToString(sum[:])[:16]
	default:
		// "timestamp" or any unrecognized value: use UTC timestamp.
		return "memory:" + o.cfg.TimeNow().UTC().Format("2006-01-02T150405.000000000Z")
	}
}

// safeExtract wraps extractCandidates in a recover boundary. A panic
// in the extractor (e.g., from a malformed third-party Content block)
// is logged and returns nil candidates so the turn continues.
func (o *memPersistObserver) safeExtract(resp *tau.Response) (candidates []string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("memory: extract panic: %v", r)
			candidates = nil
		}
	}()
	return extractCandidates(o.cfg, resp)
}

// safePut wraps Store.Put in a recover boundary. A panic or error
// from the Store backend is logged and returns nil so the observer
// continues with the next candidate.
func (o *memPersistObserver) safePut(ctx context.Context, entry tau.Entry) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("memory: put panic: %v", r)
			err = nil
		}
	}()
	if err := o.cfg.Store.Put(ctx, entry); err != nil {
		log.Printf("memory: put: %v", err)
		return nil
	}
	return nil
}
