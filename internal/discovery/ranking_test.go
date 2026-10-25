package discovery

import (
	"context"
	"errors"
	"testing"
)

type rankingFunc func(context.Context, Track, []Candidate) ([]Candidate, error)

func (f rankingFunc) Rank(ctx context.Context, t Track, c []Candidate) ([]Candidate, error) {
	return f(ctx, t, c)
}

type sourceList []Candidate

func (s sourceList) Candidates(context.Context, Track) ([]Candidate, error) { return s, nil }

func TestRankingFailureRequiresExplicitSelection(t *testing.T) {
	sources := sourceList{{Ref: Ref{Provider: "youtube", ID: "BaW_jenozKc"}, Score: 1, Suitable: true}}
	service := New(nil, sources)
	before, err := service.Candidates(context.Background(), Track{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.Select(before); !ok {
		t.Fatal("disabled ranking changed default matching")
	}
	service = New(nil, sources, WithRanking(rankingFunc(func(context.Context, Track, []Candidate) ([]Candidate, error) {
		return nil, errors.New("ranking unavailable")
	}), SelectionPolicy{MinimumScore: .9, MinimumMargin: .03}))
	after, err := service.Candidates(context.Background(), Track{})
	if err != nil || len(after) != 1 || after[0].Ref != before[0].Ref || after[0].Score != 0 || after[0].Suitable || after[0].Reason == "" {
		t.Fatal(after, err)
	}
	if _, ok := service.Select(after); ok {
		t.Fatal("provider outage triggered automatic download")
	}
	if before[0].Score != 1 || !before[0].Suitable {
		t.Fatal("fallback mutated source data")
	}
}

func TestSelectionUsesBackendPolicy(t *testing.T) {
	ref := Ref{Provider: "youtube", ID: "BaW_jenozKc"}
	candidates := []Candidate{{Ref: ref, Score: .94, Suitable: true}, {Ref: ref, Score: .9, Suitable: true}}
	if _, ok := New(nil, nil).Select(candidates); ok {
		t.Fatal("heuristic should require five point margin")
	}
	s := New(nil, nil, WithRanking(nil, SelectionPolicy{MinimumScore: .9, MinimumMargin: .03}))
	if _, ok := s.Select(candidates); !ok {
		t.Fatal("backend policy ignored")
	}
	candidates[0].Score = .89
	candidates[1].Suitable = false
	if _, ok := s.Select(candidates); ok {
		t.Fatal("low score selected")
	}
	candidates[0].Score = .94
	candidates[1].Score = .93
	candidates[1].Suitable = true
	if _, ok := s.Select(candidates); ok {
		t.Fatal("ambiguous result selected")
	}
}
