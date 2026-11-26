package search

import (
	"path"
	"strings"
)

var defaultWeights = map[FieldKind]float64{
	FieldPod:       1.00,
	FieldNamespace: 0.92,
	FieldContext:   0.86,
	FieldCluster:   0.80,
	FieldEnv:       0.82,
	FieldOwner:     0.78,
	FieldNode:      0.62,
	FieldLabel:     0.66,
	FieldContainer: 0.58,
	FieldImage:     0.50,
	FieldPhase:     0.48,
	FieldServer:    1.00,
	FieldTag:       0.88,
	FieldHost:      0.86,
	FieldUser:      0.64,
	FieldSSH:       0.80,
}

func Weight(kind FieldKind) float64 {
	if w, ok := defaultWeights[kind]; ok {
		return w
	}
	return 0.5
}

type Field struct {
	Key    FieldKind
	Value  string
	Weight float64

	norm     string
	compact  string
	tokens   []string
	prepared bool
}

func NewField(kind FieldKind, value string) Field {
	return NewFieldWeighted(kind, value, Weight(kind))
}

func NewFieldWeighted(kind FieldKind, value string, weight float64) Field {
	f := Field{Key: kind, Value: value, Weight: weight}
	if weight <= 0 {
		f.Weight = Weight(kind)
	}
	f.prepare()
	return f
}

func (f *Field) prepare() {
	if f.prepared {
		return
	}
	f.norm = normalize(f.Value)
	f.compact = compact(f.norm)
	f.tokens = splitFieldTokens(f.norm)
	f.prepared = true
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func compact(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '-', '_', '.', '/', ':', '@', ' ', '\t', '[', ']', '(', ')', ',', '=':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitFieldTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '-', '_', '.', '/', ':', '@', ' ', '\t', '[', ']', '(', ')', ',', '=':
			return true
		}
		return false
	})
}

func (f *Field) score(query, queryCompact string) float64 {
	f.prepare()
	if query == "" || f.norm == "" {
		return 0
	}
	v := f.norm
	vCompact := f.compact

	if query == v {
		return 1
	}
	if queryCompact != "" && queryCompact == vCompact {
		return 0.99
	}
	if strings.HasPrefix(v, query) {
		return 0.94 + 0.04*lengthRatio(query, v)
	}
	for _, token := range f.tokens {
		if token == query {
			return 0.97
		}
		if strings.HasPrefix(token, query) {
			return 0.93
		}
	}
	if strings.Contains(v, query) {
		return 0.85 + 0.09*lengthRatio(query, v)
	}
	if queryCompact != "" && vCompact != "" && strings.Contains(vCompact, queryCompact) {
		return 0.80 + 0.09*lengthRatio(queryCompact, vCompact)
	}
	if len(queryCompact) >= 2 && isSubsequence(queryCompact, vCompact) {
		coverage := lengthRatio(queryCompact, vCompact)
		return 0.55 + 0.25*coverage
	}

	best := 0.0
	candidates := make([]string, 0, len(f.tokens)+2)
	candidates = append(candidates, v)
	if vCompact != "" && vCompact != v {
		candidates = append(candidates, vCompact)
	}
	candidates = append(candidates, f.tokens...)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if ratio := editRatio(queryCompact, compact(candidate)); ratio >= 0.55 {
			s := 0.42 + 0.36*ratio
			if s > best {
				best = s
			}
		}
	}
	return best
}

func lengthRatio(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	ratio := float64(len(a)) / float64(len(b))
	if ratio > 1 {
		return 1
	}
	return ratio
}

func isSubsequence(sub, s string) bool {
	if sub == "" || s == "" {
		return false
	}
	i := 0
	for j := 0; j < len(s) && i < len(sub); j++ {
		if sub[i] == s[j] {
			i++
		}
	}
	return i == len(sub)
}

func editRatio(a, b string) float64 {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 1
	}
	limit := maxLen/3 + 1
	d, ok := boundedLevenshtein(a, b, limit)
	if !ok {
		return 0
	}
	return 1 - float64(d)/float64(maxLen)
}

func boundedLevenshtein(a, b string, limit int) (int, bool) {
	if a == b {
		return 0, true
	}
	if abs(len(a)-len(b)) > limit {
		return limit + 1, false
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > limit {
			return limit + 1, false
		}
		prev, curr = curr, prev
	}
	d := prev[len(b)]
	if d > limit {
		return d, false
	}
	return d, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func filterMatchesValue(value, filter string) bool {
	negate := strings.HasPrefix(filter, "!")
	if negate {
		filter = strings.TrimPrefix(filter, "!")
	}
	f := normalize(filter)
	v := normalize(value)
	result := f == ""
	if !result {
		result = v == f || strings.Contains(v, f)
	}
	if !result && strings.ContainsAny(f, "*?[") {
		if ok, err := path.Match(f, v); err == nil && ok {
			result = true
		}
	}
	if !result && strings.Contains(f, "=") {
		parts := strings.SplitN(f, "=", 2)
		result = strings.Contains(v, parts[0]) && strings.Contains(v, parts[1])
	}
	if negate {
		return !result
	}
	return result
}
