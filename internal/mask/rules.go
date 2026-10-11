// Package mask rewrites recognized text lines in a screenshot: lines whose
// text matches a replacement rule are painted over with the sampled
// background and the substituted text is drawn back in place.
//
// Everything here is pure Go (image/draw + x/image/font), so it compiles and
// tests without cgo or the OCR runtime.
package mask

import (
	"sort"
	"strings"
)

// Rule is one literal substitution. Matching is case-sensitive, so a
// dictionary that needs both "NutCorp" and "nutcorp" lists both, exactly as
// the original Python REPLACE_DICT did.
type Rule struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DefaultRules is the dictionary the original tool shipped with. It is the
// starting point the UI shows; callers pass their own list to Apply.
func DefaultRules() []Rule {
	return []Rule{
		{"NutCorp", "SquirrelCorp"},
		{"nutcorp", "squirrelcorp"},
		{"DevOps", "SysAdmin"},
		{"devops", "sysadmin"},
		{"github", "screenpear"},
		{"tesseract", "EasyOCR"},
		{"postgres", "sqlite"},
		{"Upgrade", "update"},
		{"Orange", "Raspberry"},
		{"metameetings", "instabark"},
		{"mapdigital", "barkdigital"},
		{"mm-core", "ib-core"},
		{"mm-", "ib-"},
	}
}

// Rewrite applies every rule to text, longest From first so "mm-core" wins
// over "mm-" when both are present. It returns the new text and whether
// anything changed.
func Rewrite(text string, rules []Rule) (string, bool) {
	ordered := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.From != "" {
			ordered = append(ordered, r)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].From) > len(ordered[j].From)
	})
	out := text
	for _, r := range ordered {
		out = strings.ReplaceAll(out, r.From, r.To)
	}
	return out, out != text
}

// ParseRules reads "from=to" lines, one rule per line. Blank lines and lines
// starting with # are skipped; a line without "=" is an error-free no-op so a
// half-typed rule in the UI never blocks a run.
func ParseRules(s string) []Rule {
	var rules []Rule
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		from, to, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		from = strings.TrimSpace(from)
		if from == "" {
			continue
		}
		rules = append(rules, Rule{From: from, To: strings.TrimSpace(to)})
	}
	return rules
}

// FormatRules is the inverse of ParseRules.
func FormatRules(rules []Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.From)
		b.WriteString("=")
		b.WriteString(r.To)
		b.WriteString("\n")
	}
	return b.String()
}
