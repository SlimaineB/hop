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
	return e.RankMin(q, candidates, limit, 0)
}

type preparedTerm struct {
	raw        string
	normalized string
	compact    string
}

func prepareTerms(terms []string) []preparedTerm {
	prepared := make([]preparedTerm, len(terms))
	for i, term := range terms {
		normalized := normalize(term)
		prepared[i] = preparedTerm{
			raw:        term,
			normalized: normalized,
			compact:    compact(normalized),
		}
	}
	return prepared
}

// RankMin ranks candidates and drops scored hits below minScore. A neutral
// query (no terms) is never filtered, since those hits all score zero.
func (e *Engine) RankMin(q Query, candidates []Candidate, limit int, minScore float64) []Hit {
	terms := prepareTerms(q.Terms)
	neutral := len(terms) == 0
	hits := make([]Hit, 0, len(candidates))
	for i := range candidates {
		c := &candidates[i]
		if !candidateMatchesFilters(c, q.Filters) {
			continue
		}
		if neutral {
			hits = append(hits, Hit{Candidate: *c})
			continue
		}
		hits = append(hits, Hit{Candidate: *c, Score: scoreCandidate(terms, c)})
	}

	if !neutral {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].Score == hits[j].Score {
				return hits[i].Candidate.ID < hits[j].Candidate.ID
			}
			return hits[i].Score > hits[j].Score
		})
		if minScore > 0 {
			kept := hits[:0]
			for _, hit := range hits {
				if hit.Score >= minScore {
					kept = append(kept, hit)
				}
			}
			hits = kept
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	if !neutral {
		for i := range hits {
			annotateHit(terms, &hits[i])
		}
	}
	return hits
}

func scoreCandidate(terms []preparedTerm, c *Candidate) float64 {
	sum := 0.0
	matched := 0
	firstMatchedField := -1
	allInOne := true

	for _, term := range terms {
		best := 0.0
		bestIndex := -1
		for i := range c.Fields {
			s := c.Fields[i].score(term.normalized, term.compact) * c.Fields[i].Weight
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
	}

	avg := sum / float64(len(terms))
	coverage := float64(matched) / float64(len(terms))
	score := avg * (0.45 + 0.55*coverage)
	if matched == len(terms) && len(terms) > 1 {
		score = minFloat(1, score*1.06+0.01)
	}
	if allInOne && firstMatchedField >= 0 && matched == len(terms) {
		score = minFloat(1, score+0.02)
	}
	return minFloat(1, score+c.Boost)
}

func annotateHit(terms []preparedTerm, hit *Hit) {
	matches := make([]Match, 0, len(terms))
	for _, term := range terms {
		best := 0.0
		bestIndex := -1
		for i := range hit.Candidate.Fields {
			s := hit.Candidate.Fields[i].score(term.normalized, term.compact) * hit.Candidate.Fields[i].Weight
			if s > best {
				best = s
				bestIndex = i
			}
		}
		if bestIndex >= 0 && best > 0 {
			matches = append(matches, Match{
				Field: hit.Candidate.Fields[bestIndex].Key,
				Value: hit.Candidate.Fields[bestIndex].Value,
				Term:  term.raw,
				Score: best,
			})
		}
	}
	hit.Matches = matches
	hit.Reasons = summarizeMatches(matches)
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
