package main

import (
	"fmt"
	"strings"
)

// modelResult is one candidate model's run outcome: metrics on success, Err on
// failure. Content (the digest text) lives only in its own file — never here.
type modelResult struct {
	Model       string
	TokensIn    int
	TokensOut   int
	DurationMs  int64
	DigestChars int
	Truncated   bool
	Err         error
}

// sanitizeModel turns a model id like "openai/gpt-5" into a filesystem-safe
// filename fragment ("openai_gpt-5"): OpenRouter model ids are "vendor/model"
// and "/" cannot appear in a filename.
func sanitizeModel(model string) string { return strings.ReplaceAll(model, "/", "_") }

// splitModels splits a comma-separated model list, trimming spaces and
// dropping empty entries (e.g. from a trailing comma).
func splitModels(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// formatSummary renders results as a markdown table: | model | tokens_in |
// tokens_out | duration_ms | digest_chars | truncated |. A model whose Err is
// set gets "error" in every metric cell instead of numbers, so a failed
// candidate stays visible without polluting successful rows with zeros.
func formatSummary(results []modelResult) string {
	var b strings.Builder
	b.WriteString("| model | tokens_in | tokens_out | duration_ms | digest_chars | truncated |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(&b, "| %s | error | error | error | error | error |\n", r.Model)
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %t |\n",
			r.Model, r.TokensIn, r.TokensOut, r.DurationMs, r.DigestChars, r.Truncated)
	}
	return b.String()
}
