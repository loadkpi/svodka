package digest

import (
	"regexp"
	"strings"
)

// backlinkRe matches both backlink formats built by linkFor (M28): the
// private https://t.me/c/<channel id>/<message id> and the public
// https://t.me/<username>/<message id>. The "c/" literal alternative is tried
// first, so a private link is never misparsed as a username segment; Telegram
// usernames are 5-32 chars of [A-Za-z0-9_], so a bare "c" can never collide
// with the private form's literal "c" path segment.
var backlinkRe = regexp.MustCompile(`https://t\.me/(?:c/(\d+)|([A-Za-z0-9_]{5,32}))/(\d+)`)

// spaceRunRe matches a run of two or more horizontal spaces, left behind once
// a link substring is removed from the middle of a line.
var spaceRunRe = regexp.MustCompile(`[ \t]{2,}`)

// sanitizeLinks is a deterministic backstop against a misquoted or
// reformatted backlink (eval 2026-07-11: the model can transpose a digit in a
// message id — the prompt discourages it but does not guarantee it). It
// strips any t.me backlink, public or private, whose exact URL is not in
// valid — this also catches a link "downgraded" to the private c/ form for a
// chat that only ever had a public one (or vice versa), since valid holds the
// one canonical URL linkFor would have produced. It leaves the surrounding
// bullet text in place and returns the cleaned text plus the number of links
// removed (M27, M28).
func sanitizeLinks(text string, valid map[string]bool) (string, int) {
	removed := 0
	cleaned := backlinkRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := backlinkRe.FindStringSubmatch(match)
		msgID := sub[3]
		var key string
		switch {
		case sub[1] != "":
			key = "https://t.me/c/" + sub[1] + "/" + msgID
		case sub[2] != "":
			key = "https://t.me/" + sub[2] + "/" + msgID
		}
		if key != "" && valid[key] {
			return match
		}
		removed++
		return ""
	})
	if removed == 0 {
		return text, 0
	}
	return tidyWhitespace(cleaned), removed
}

// tidyWhitespace cleans up the gap a removed link leaves behind: a run of
// spaces/tabs collapses to one, and trailing space before each line's end is
// dropped.
func tidyWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = spaceRunRe.ReplaceAllString(line, " ")
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}
