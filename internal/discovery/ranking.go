package discovery

import (
	"context"
	"log/slog"
	"math"
)

type Ranker interface {
	Rank(context.Context, Track, []Candidate) ([]Candidate, error)
}

type SelectionPolicy struct {
	MinimumScore  float64
	MinimumMargin float64
}

type Option func(*Service)

func WithRanking(ranker Ranker, policy SelectionPolicy) Option {
	return func(s *Service) { s.ranker, s.policy = ranker, policy }
}

// Select accepts only a strong, unambiguous candidate under this backend's policy.
func (s *Service) Select(candidates []Candidate) (Ref, bool) {
	if len(candidates) == 0 || !candidates[0].Suitable || !candidates[0].Ref.ValidSource() || !validScore(candidates[0].Score) || candidates[0].Score < s.policy.MinimumScore {
		return Ref{}, false
	}
	for _, next := range candidates[1:] {
		if next.Suitable && (!validScore(next.Score) || candidates[0].Score-next.Score < s.policy.MinimumMargin) {
			return Ref{}, false
		}
	}
	return candidates[0].Ref, true
}

func validScore(score float64) bool { return !math.IsNaN(score) && score >= 0 && score <= 1 }

func (s *Service) rank(ctx context.Context, track Track, candidates []Candidate) ([]Candidate, error) {
	ranked, err := s.ranker.Rank(ctx, track, candidates)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil {
		return ranked, nil
	}
	// A configured backend failure must not silently authorize a heuristic match.
	// Preserve the sources for an explicit choice through the existing API.
	slog.Warn("recording ranking unavailable; manual selection required", "error", err)
	manual := append([]Candidate(nil), candidates...)
	for i := range manual {
		manual[i].Score = 0
		manual[i].Suitable = false
		manual[i].Reason = "Automatic ranking unavailable; choose a recording manually"
	}
	return manual, nil
}
