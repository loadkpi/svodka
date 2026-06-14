package digest

import (
	"fmt"
	"strings"
)

// The system prompts are deliberately plain and parameterized only by output
// language. The map prompt is reused byte-for-byte across every map call so the
// cached system block (set in claude.go) pays off. Output formatting targets
// Telegram: plain text, simple "- " bullets, no markdown headers/tables/code
// fences that Telegram would render literally.

// formatRules is shared across prompts so output styling stays consistent.
const formatRules = "Use plain text only. No markdown headers (#), tables, or code fences. " +
	"Use '- ' for bullet points. Put each chat title on its own line. " +
	"Be factual and concise; omit greetings and small talk; do not invent facts. " +
	"Source links: input lines may end with a bare link (https://t.me/...). Every bullet " +
	"that summarizes linked messages MUST end with the link of its most relevant source " +
	"message, copied verbatim. Keep links already present on input bullets; never invent " +
	"or alter links."

// withExtra appends the deployer's free-text extra_instructions to a system
// prompt. Empty extra returns base unchanged, so the no-customization case stays
// byte-for-byte as before. The formatting rules above take precedence, so extra
// can shape content/tone/structure but not break Telegram-safe output.
func withExtra(base, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	return base + " Additional user instructions (follow them unless they conflict with the rules above): " + extra
}

// singleSystem instructs the model to summarize all chats in one pass, keeping a
// per-chat structure. User extra_instructions (if any) shape the final output.
func singleSystem(lang, extra string) string {
	return withExtra(fmt.Sprintf(
		"You produce a daily digest of Telegram chats. Write everything in language %q (ISO 639-1 code). "+
			"The input lists chats; each starts with a '## ' title marker, followed by '[HH:MM] Author: text' lines (a line may end with a source link). "+
			"For each chat, output the chat title on its own line, then bullets covering key topics, decisions, "+
			"mentions of the account owner, and important links. %s",
		lang, formatRules,
	), extra)
}

// mapSystem instructs the model to summarize a single chat. The output has no
// title (the reduce step adds it). extra_instructions are deliberately NOT
// applied here: map summaries are intermediate, so a "be brief" instruction must
// not over-compress them before reduce sees the material (it shapes single/reduce
// instead). Keeping mapSystem untouched also preserves its byte-identity across
// map calls for prompt caching (ADR-10).
func mapSystem(lang string) string {
	return fmt.Sprintf(
		"Summarize one Telegram chat into bullet points. Write everything in language %q (ISO 639-1 code). "+
			"The input starts with a '## ' title marker, followed by '[HH:MM] Author: text' lines (a line may end with a source link). "+
			"Output only the bullets — key topics, decisions, mentions of the account owner, important links. "+
			"Do not repeat the chat title. %s",
		lang, formatRules,
	)
}

// reduceSystem instructs the model to assemble per-chat summaries into the final
// digest, preserving structure and only lightly deduplicating. extra_instructions
// (if any) shape the final output here.
func reduceSystem(lang, extra string) string {
	return withExtra(fmt.Sprintf(
		"You assemble a final daily digest from per-chat summaries. Write everything in language %q (ISO 639-1 code). "+
			"The input lists summaries; each starts with a '## ' title marker. Keep the per-chat structure: "+
			"the chat title on its own line, then its bullets. Only reorganize and lightly deduplicate; "+
			"do not add new facts. %s",
		lang, formatRules,
	), extra)
}
