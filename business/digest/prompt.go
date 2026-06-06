package digest

import "fmt"

// The system prompts are deliberately plain and parameterized only by output
// language. The map prompt is reused byte-for-byte across every map call so the
// cached system block (set in claude.go) pays off. Output formatting targets
// Telegram: plain text, simple "- " bullets, no markdown headers/tables/code
// fences that Telegram would render literally.

// formatRules is shared across prompts so output styling stays consistent.
const formatRules = "Use plain text only. No markdown headers (#), tables, or code fences. " +
	"Use '- ' for bullet points. Put each chat title on its own line. " +
	"Be factual and concise; omit greetings and small talk; do not invent facts."

// singleSystem instructs the model to summarize all chats in one pass, keeping a
// per-chat structure.
func singleSystem(lang string) string {
	return fmt.Sprintf(
		"You produce a daily digest of Telegram chats. Write everything in language %q (ISO 639-1 code). "+
			"The input lists chats; each starts with a '## ' title marker, followed by '[HH:MM] Author: text' lines. "+
			"For each chat, output the chat title on its own line, then bullets covering key topics, decisions, "+
			"mentions of the account owner, and important links. %s",
		lang, formatRules,
	)
}

// mapSystem instructs the model to summarize a single chat. The output has no
// title (the reduce step adds it).
func mapSystem(lang string) string {
	return fmt.Sprintf(
		"Summarize one Telegram chat into bullet points. Write everything in language %q (ISO 639-1 code). "+
			"The input starts with a '## ' title marker, followed by '[HH:MM] Author: text' lines. "+
			"Output only the bullets — key topics, decisions, mentions of the account owner, important links. "+
			"Do not repeat the chat title. %s",
		lang, formatRules,
	)
}

// reduceSystem instructs the model to assemble per-chat summaries into the final
// digest, preserving structure and only lightly deduplicating.
func reduceSystem(lang string) string {
	return fmt.Sprintf(
		"You assemble a final daily digest from per-chat summaries. Write everything in language %q (ISO 639-1 code). "+
			"The input lists summaries; each starts with a '## ' title marker. Keep the per-chat structure: "+
			"the chat title on its own line, then its bullets. Only reorganize and lightly deduplicate; "+
			"do not add new facts. %s",
		lang, formatRules,
	)
}
