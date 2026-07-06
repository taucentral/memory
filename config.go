// config.go — configuration for the memory plugin.
//
// The Config struct is shared between the retrieve mutator and the
// persist observer so both use symmetric scope tags and ID
// conventions. New(cfg) calls applyDefaults before constructing the
// middleware, so a zero-value Config (with just Store set) works.
package memory

import (
	"time"

	tau "github.com/taucentral/tau/pkg/tau"
)

// Config configures the memory plugin. Only Store is required; all
// other fields have sensible defaults populated by applyDefaults.
type Config struct {
	// Store is the tau.Store the plugin reads from and writes to.
	// When nil, both the mutator and observer are no-ops.
	Store tau.Store

	// Scope controls tag-based partitioning. One of:
	//   "session"  — tag "session:"+SourceID
	//   "project"  — tag "project:"+sha256(cwd)[:8]
	//   "user"     — tag "user:"+UserID
	//   "global"   — no scope tag (only "memory")
	//   ""         — same as "global"
	Scope string

	// UserID is the user identifier used for the "user" scope tag.
	UserID string

	// SourceID is the session identifier used for the "session" scope
	// tag and as the Entry.Source field on persisted entries.
	SourceID string

	// RetrieveLimit is the maximum number of entries the mutator
	// retrieves per turn. Default 10.
	RetrieveLimit int

	// RetrieveHeader is the text prepended to the injected memory
	// block. Default "Relevant memory entries:".
	RetrieveHeader string

	// ExtractionMode controls how the observer extracts candidates
	// from the response. One of:
	//   "full_response" — concatenate all TextContent blocks (default)
	//   "marker"        — extract between MarkerOpen/MarkerClose
	ExtractionMode string

	// MarkerOpen is the opening delimiter for marker extraction.
	// Default "<memory>".
	MarkerOpen string

	// MarkerClose is the closing delimiter for marker extraction.
	// Default "</memory>".
	MarkerClose string

	// IDMode controls how entry IDs are generated. One of:
	//   "content_hash" — "memory:"+sha256(text)[:16] (default; dedup)
	//   "timestamp"    — "memory:"+UTC timestamp (no dedup)
	IDMode string

	// UpdateTimestampOnContentHash controls whether a content_hash
	// Put collision refreshes the existing entry's timestamp. When
	// true (default), the second Put overwrites the first, refreshing
	// the timestamp. When false, the original entry is preserved
	// verbatim (timestamp included) — the second Put is skipped.
	// Only meaningful with IDMode == "content_hash"; ignored for
	// "timestamp" mode (which always produces distinct IDs).
	UpdateTimestampOnContentHash bool

	// Redactor is an optional hook called on each extracted
	// candidate before Put. A false return skips the entry; the
	// returned string replaces the text. Default nil (no redaction).
	Redactor func(text string) (string, bool)

	// TimeNow is the clock source for entry timestamps and timestamp
	// ID generation. Default time.Now.
	TimeNow func() time.Time
}

// applyDefaults populates zero-value fields with sensible defaults.
// It is called by New before constructing the middleware.
func (c *Config) applyDefaults() {
	if c.RetrieveLimit == 0 {
		c.RetrieveLimit = 10
	}
	if c.RetrieveHeader == "" {
		c.RetrieveHeader = "Relevant memory entries:"
	}
	if c.ExtractionMode == "" {
		c.ExtractionMode = "full_response"
	}
	if c.MarkerOpen == "" {
		c.MarkerOpen = "<memory>"
	}
	if c.MarkerClose == "" {
		c.MarkerClose = "</memory>"
	}
	if c.IDMode == "" {
		c.IDMode = "content_hash"
	}
	// UpdateTimestampOnContentHash defaults to true. The zero value
	// of a bool is false, so we cannot distinguish "not set" from
	// "explicitly false" — embedders who want false must set it
	// before calling New. New calls applyDefaults, so the common
	// path gets true. This matches the documented default.
	c.UpdateTimestampOnContentHash = true
	if c.TimeNow == nil {
		c.TimeNow = time.Now
	}
}
