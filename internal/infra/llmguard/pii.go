package llmguard

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

const (
	entityEmail      = "email"
	entityPhone      = "phone"
	entityCNMobile   = "cn_mobile"
	entityCNID       = "cn_id"
	entityCreditCard = "credit_card"
)

var (
	emailRE     = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)
	cnMobileRE  = regexp.MustCompile(`(^|[^0-9])((?:\+?86[ -]?)?1[3-9]\d{9})([^0-9]|$)`)
	phoneRE     = regexp.MustCompile(`(^|[^0-9A-Za-z])(\+\d{1,3}[ -]?\d{3,14})([^0-9A-Za-z]|$)`)
	cnIDRE      = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z])(\d{17}[\dX])([^0-9A-Za-z]|$)`)
	cardSeqRE   = regexp.MustCompile(`(?:\d[ -]?){13,19}`)
	nonDigitRE  = regexp.MustCompile(`\D`)
	placeholder = map[string]string{
		entityEmail:      "<PII:email>",
		entityPhone:      "<PII:phone>",
		entityCNMobile:   "<PII:cn_mobile>",
		entityCNID:       "<PII:cn_id>",
		entityCreditCard: "<PII:credit_card>",
	}
)

type PIIGuard struct{}

func NewPIIGuard() PIIGuard { return PIIGuard{} }

func (PIIGuard) Name() string { return "pii" }

func (PIIGuard) Apply(_ context.Context, input GuardInput) (GuardResult, error) {
	matches := collectPIIMatches(input.Text, input.Rules)
	findings := summarizeMatches(matches)
	if hasBlockingMatch(matches) {
		return GuardResult{Text: input.Text, Blocked: true, Findings: findings}, nil
	}
	return GuardResult{
		Text:     redactMatches(input.Text, matches),
		Findings: findings,
	}, nil
}

type piiMatch struct {
	start  int
	end    int
	entity string
	action Action
	repl   string
}

func collectPIIMatches(text string, rules []Rule) []piiMatch {
	var out []piiMatch
	for _, r := range rules {
		entity := ""
		if len(r.Entities) > 0 {
			entity = r.Entities[0]
		}
		out = append(out, detectEntity(text, entity, r)...)
	}
	return mergePIIMatches(out)
}

func detectEntity(text, entity string, r Rule) []piiMatch {
	switch entity {
	case entityEmail:
		return simpleRegexMatches(text, emailRE, entity, r)
	case entityPhone:
		return capturedRegexMatches(text, phoneRE, entity, r)
	case entityCNMobile:
		return capturedRegexMatches(text, cnMobileRE, entity, r)
	case entityCNID:
		return capturedRegexMatches(text, cnIDRE, entity, r)
	case entityCreditCard:
		return cardMatches(text, r)
	default:
		return nil
	}
}

func simpleRegexMatches(text string, re *regexp.Regexp, entity string, r Rule) []piiMatch {
	idxs := re.FindAllStringIndex(text, -1)
	out := make([]piiMatch, 0, len(idxs))
	for _, idx := range idxs {
		out = append(out, buildPIIMatch(idx[0], idx[1], entity, r))
	}
	return out
}

func capturedRegexMatches(text string, re *regexp.Regexp, entity string, r Rule) []piiMatch {
	idxs := re.FindAllStringSubmatchIndex(text, -1)
	out := make([]piiMatch, 0, len(idxs))
	for _, idx := range idxs {
		start, end := capturedPIISpan(idx)
		if start < 0 || end < 0 {
			continue
		}
		out = append(out, buildPIIMatch(start, end, entity, r))
	}
	return out
}

func capturedPIISpan(idx []int) (int, int) {
	for group := 2; group >= 1; group-- {
		start := group * 2
		end := start + 1
		if len(idx) > end && idx[start] >= 0 && idx[end] >= 0 {
			return idx[start], idx[end]
		}
	}
	return -1, -1
}

func cardMatches(text string, r Rule) []piiMatch {
	idxs := cardSeqRE.FindAllStringIndex(text, -1)
	out := make([]piiMatch, 0, len(idxs))
	for _, idx := range idxs {
		candidate := text[idx[0]:idx[1]]
		digits := nonDigitRE.ReplaceAllString(candidate, "")
		if len(digits) < 13 || len(digits) > 19 || !luhnValid(digits) || allSameDigit(digits) {
			continue
		}
		out = append(out, buildPIIMatch(idx[0], idx[1], entityCreditCard, r))
	}
	return out
}

func buildPIIMatch(start, end int, entity string, r Rule) piiMatch {
	repl := r.Replacement
	if repl == "" {
		repl = placeholder[entity]
	}
	repl = strings.ReplaceAll(repl, "{entity}", entity)
	return piiMatch{start: start, end: end, entity: entity, action: r.Action, repl: repl}
}

func mergePIIMatches(in []piiMatch) []piiMatch {
	sort.Slice(in, func(i, j int) bool {
		if in[i].start == in[j].start {
			return actionRank(in[i].action) > actionRank(in[j].action)
		}
		return in[i].start < in[j].start
	})
	out := make([]piiMatch, 0, len(in))
	for _, m := range in {
		if len(out) == 0 || m.start >= out[len(out)-1].end {
			out = append(out, m)
			continue
		}
		last := out[len(out)-1]
		if actionRank(m.action) > actionRank(last.action) || m.end > last.end {
			out[len(out)-1] = strongerPIIMatch(last, m)
		}
	}
	return out
}

func strongerPIIMatch(a, b piiMatch) piiMatch {
	if actionRank(b.action) > actionRank(a.action) {
		return b
	}
	if b.end-b.start > a.end-a.start {
		return b
	}
	return a
}

func summarizeMatches(matches []piiMatch) []Finding {
	counts := map[string]int{}
	actions := map[string]Action{}
	for _, m := range matches {
		if m.action == ActionOff {
			continue
		}
		key := m.entity + "/" + string(m.action)
		counts[key]++
		actions[m.entity] = m.action
	}
	out := make([]Finding, 0, len(counts))
	for key, n := range counts {
		parts := strings.Split(key, "/")
		out = append(out, Finding{Guard: "pii", Entity: parts[0], Action: actions[parts[0]], Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
	return out
}

func hasBlockingMatch(matches []piiMatch) bool {
	for _, m := range matches {
		if m.action == ActionBlock {
			return true
		}
	}
	return false
}

func redactMatches(text string, matches []piiMatch) string {
	var b strings.Builder
	pos := 0
	for _, m := range matches {
		if m.action != ActionRedact {
			continue
		}
		b.WriteString(text[pos:m.start])
		b.WriteString(m.repl)
		pos = m.end
	}
	if pos == 0 {
		return text
	}
	b.WriteString(text[pos:])
	return b.String()
}

func luhnValid(s string) bool {
	sum := 0
	double := false
	for i := len(s) - 1; i >= 0; i-- {
		n := int(s[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0
}

func allSameDigit(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}
