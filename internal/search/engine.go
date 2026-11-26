package search

import (
	"sort"
	"strings"
)

type Candidate struct {
	ID     string
	Fields []Field
	Boost  float64
}

func NewCandidate(id string, fields ...Field) Candidate {
	return Candidate{ID: id, Fields: fields}
}

func (c *Candidate) Add(kind FieldKind, value string) {
	if value == "" {
		return
	}
	c.Fields = append(c.Fields, NewField(kind, value))
}

type Match struct {
	Field FieldKind `json:"field"`
	Value string    `json:"value"`
	Term  string    `json:"term"`
	Score float64   `json:"score"`
}

type Hit struct {
	Candidate Candidate `json:"-"`
	Score     float64   `json:"score"`
	Matches   []Match   `json:"matches,omitempty"`
	Reasons   []string  `json:"reasons,omitempty"`
}

func (h Hit) Percent() int {
	return int(h.Score*100 + 0.5)
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Rank(q Query, candidates []Candidate, limit int) []Hit {
	hits := make([]Hit, 0, len(candidates))
	neutral := len(q.Terms) == 0
	for i := range candidates {
		c := &candidates[i]
		if !candidateMatchesFilters(c, q.Filters) {
			continue
		}
		if neutral {
			hits = append(hits, Hit{Candidate: *c})
			continue
		}
		hits = append(hits, scoreCandidate(q, c))
	}

	if !neutral {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].Score == hits[j].Score {
				return hits[i].Candidate.ID < hits[j].Candidate.ID
			}
			return hits[i].Score > hits[j].Score
		})
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func scoreCandidate(q Query, c *Candidate) Hit {
	matches := make([]Match, 0, len(q.Terms))
	sum := 0.0
	matched := 0
	firstMatchedField := -1
	allInOne := true

	for _, term := range q.Terms {
		normalized := normalize(term)
		compactTerm := compact(normalized)
		best := 0.0
		bestIndex := -1
		for i := range c.Fields {
			s := c.Fields[i].score(normalized, compactTerm) * c.Fields[i].Weight
			if s > best {
				best = s
				bestIndex = i
			}
		}
		if best > 0.15 {
			matched++
			if firstMatchedField == -1 {
				firstMatchedField = bestIndex
			} else if bestIndex != firstMatchedField {
				allInOne = false
			}
		} else {
			allInOne = false
		}
		sum += best
		if bestIndex >= 0 && best > 0 {
			matches = append(matches, Match{
				Field: c.Fields[bestIndex].Key,
				Value: c.Fields[bestIndex].Value,
				Term:  term,
				Score: best,
			})
		}
	}

	avg := sum / float64(len(q.Terms))
	coverage := float64(matched) / float64(len(q.Terms))
	score := avg * (0.45 + 0.55*coverage)
	if matched == len(q.Terms) && len(q.Terms) > 1 {
		score = minFloat(1, score*1.06+0.01)
	}
	if allInOne && firstMatchedField >= 0 && matched == len(q.Terms) {
		score = minFloat(1, score+0.02)
	}
	score = minFloat(1, score+c.Boost)

	return Hit{
		Candidate: *c,
		Score:     score,
		Matches:   matches,
		Reasons:   summarizeMatches(matches),
	}
}

func summarizeMatches(matches []Match) []string {
	seen := map[FieldKind]bool{}
	reasons := make([]string, 0, 4)
	for _, m := range matches {
		if seen[m.Field] {
			continue
		}
		seen[m.Field] = true
		value := m.Value
		if len(value) > 56 {
			value = value[:53] + "..."
		}
		reasons = append(reasons, string(m.Field)+":"+value)
		if len(reasons) == 4 {
			break
		}
	}
	return reasons
}

func candidateMatchesFilters(c *Candidate, filters map[FieldKind][]string) bool {
	if len(filters) == 0 {
		return true
	}
	for kind, list := range filters {
		if len(list) == 0 {
			continue
		}
		and := kind == FieldLabel || kind == FieldContainer || kind == FieldImage || kind == FieldTag
		satisfied := 0
		for _, filter := range list {
			matchedAny := false
			for i := range c.Fields {
				if c.Fields[i].Key != kind {
					continue
				}
				if filterMatchesValue(c.Fields[i].Value, filter) {
					matchedAny = true
					break
				}
			}
			if matchedAny {
				satisfied++
			} else if and {
				return false
			}
		}
		if satisfied == 0 {
			return false
		}
	}
	return true
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func JoinReasons(reasons []string) string {
	return strings.Join(reasons, " · ")
}
