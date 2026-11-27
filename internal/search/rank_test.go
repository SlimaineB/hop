package search

import "testing"

func TestRankMinDropsWeakMatches(t *testing.T) {
	candidates := []Candidate{
		NewCandidate("exact", NewField(FieldPod, "api")),
		NewCandidate("weak", NewField(FieldPod, "zzz")),
	}
	q := Query{Terms: []string{"api"}}

	all := NewEngine().RankMin(q, candidates, 0, 0)
	if len(all) != 2 {
		t.Fatalf("unfiltered hits = %d, want 2", len(all))
	}
	filtered := NewEngine().RankMin(q, candidates, 0, 0.5)
	if len(filtered) != 1 || filtered[0].Candidate.ID != "exact" {
		t.Fatalf("filtered hits = %+v, want exact only", filtered)
	}
	limited := NewEngine().RankMin(q, candidates, 1, 0.5)
	if len(limited) != 1 || limited[0].Candidate.ID != "exact" {
		t.Fatalf("limited hits = %+v, want exact only", limited)
	}
}

func TestRankMinKeepsNeutralQuery(t *testing.T) {
	candidates := []Candidate{
		NewCandidate("a", NewField(FieldPod, "api")),
		NewCandidate("b", NewField(FieldPod, "worker")),
	}
	hits := NewEngine().RankMin(Query{}, candidates, 0, 0.9)
	if len(hits) != 2 {
		t.Fatalf("neutral hits = %d, want 2", len(hits))
	}
}
