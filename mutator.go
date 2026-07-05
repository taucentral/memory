// mutator.go — the retrieve mutator (RequestMutator).
//
// Queries the Store by scope tags and prepends matching entries to
// req.System as a single TextContent block. Errors are swallowed
// (non-aborting) per the asymmetric contract: "no injected memory"
// is a safe degradation; "abort the turn" is not.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"strings"

	tau "github.com/coevin/tau/pkg/tau"
)

// memRetrieveMutator queries the Store for memory entries matching
// the configured scope tags and prepends them to req.System.
type memRetrieveMutator struct {
	cfg Config
}

// MutateRequest implements tau.RequestMutator. It queries the Store,
// formats matching entries as a bullet list, and prepends the result
// to req.System as a tau.TextContent block.
//
// Errors from Store.Query are logged and swallowed (non-aborting).
// An empty result, nil Store, or unsupported-query error leaves
// req.System unchanged.
func (m *memRetrieveMutator) MutateRequest(ctx context.Context, req *tau.Request) error {
	if m.cfg.Store == nil {
		return nil
	}
	tags := scopeTags(m.cfg)
	entries, err := m.cfg.Store.Query(ctx, tau.Query{
		TagsQuery: tags,
		Limit:     m.cfg.RetrieveLimit,
	})
	if err != nil {
		if errors.Is(err, tau.ErrUnsupportedQuery) {
			// Future backends that reject tag queries degrade
			// silently rather than aborting every turn.
			return nil
		}
		log.Printf("memory: query: %v", err)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(m.cfg.RetrieveHeader)
	sb.WriteString("\n")
	for _, e := range entries {
		sb.WriteString("- ")
		sb.WriteString(e.Text)
		sb.WriteString("\n")
	}
	// Prepend to req.System so the model sees memory before any
	// existing system-prompt blocks.
	req.System = append([]tau.ContentBlock{
		tau.TextContent{Text: sb.String()},
	}, req.System...)
	return nil
}

// scopeTags returns the tag set used by both the retrieve mutator
// (for Query) and the persist observer (for Put). The first tag is
// always "memory" so a cross-scope query using TagsQuery: ["memory"]
// reads all entries regardless of scope.
//
// Per cfg.Scope:
//   - "session": adds "session:"+cfg.SourceID
//   - "project": adds "project:"+sha256(cwd)[:8]
//   - "user":    adds "user:"+cfg.UserID
//   - "global" or "": no scope tag (only "memory")
func scopeTags(cfg Config) []string {
	tags := []string{"memory"}
	switch cfg.Scope {
	case "session":
		if cfg.SourceID != "" {
			tags = append(tags, "session:"+cfg.SourceID)
		}
	case "project":
		cwd, err := os.Getwd()
		if err != nil {
			// If cwd is unavailable, fall back to the bare
			// "memory" tag so the plugin still functions.
			return tags
		}
		sum := sha256.Sum256([]byte(cwd))
		tags = append(tags, "project:"+hex.EncodeToString(sum[:])[:8])
	case "user":
		if cfg.UserID != "" {
			tags = append(tags, "user:"+cfg.UserID)
		}
	default:
		// "global" or "": no scope tag.
	}
	return tags
}
