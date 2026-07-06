// extract.go — extraction modes for the persist observer.
//
// extractCandidates is the dispatch point: it iterates resp.Content
// for TextContent blocks, accumulates the full text, then switches on
// cfg.ExtractionMode to return either the full text (full_response)
// or the content between configurable markers (marker).
package memory

import (
	"strings"

	tau "github.com/taucentral/tau/pkg/tau"
)

// extractCandidates returns zero-or-more memory candidate strings
// extracted from resp per the configured ExtractionMode.
//
//   - "full_response" (default) / "": returns the concatenation of
//     all TextContent blocks as a single-element slice (when non-empty).
//   - "marker": returns one candidate per <MarkerOpen>...</MarkerClose>
//     region, with whitespace trimmed and empty regions skipped.
//   - Any other value returns nil.
func extractCandidates(cfg Config, resp *tau.Response) []string {
	if resp == nil {
		return nil
	}
	var fullText strings.Builder
	for _, b := range resp.Content {
		if tc, ok := b.(tau.TextContent); ok {
			fullText.WriteString(tc.Text)
		}
	}
	text := fullText.String()
	switch cfg.ExtractionMode {
	case "full_response", "":
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	case "marker":
		return extractBetweenMarkers(text, cfg.MarkerOpen, cfg.MarkerClose)
	default:
		return nil
	}
}

// extractBetweenMarkers finds all non-overlapping regions delimited
// by open and close in text, returning the trimmed content of each
// non-empty region. Malformed markers (open without close) produce
// no candidate for that unmatched open.
func extractBetweenMarkers(text, open, close string) []string {
	if open == "" || close == "" {
		return nil
	}
	var out []string
	searchFrom := 0
	for {
		openIdx := strings.Index(text[searchFrom:], open)
		if openIdx < 0 {
			break
		}
		openIdx += searchFrom
		contentStart := openIdx + len(open)
		closeIdx := strings.Index(text[contentStart:], close)
		if closeIdx < 0 {
			// Unmatched open; stop searching.
			break
		}
		closeIdx += contentStart
		region := strings.TrimSpace(text[contentStart:closeIdx])
		if region != "" {
			out = append(out, region)
		}
		searchFrom = closeIdx + len(close)
	}
	return out
}
