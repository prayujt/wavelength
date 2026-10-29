package playfeed

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/telemetry"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

var requestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

type pool struct {
	tracks []library.Track
	scores map[string]float64
	at     time.Time
	source string
}
type userLimit struct {
	limiter *rate.Limiter
	at      time.Time
}
type Service struct {
	ctx     context.Context
	lib     *library.Library
	store   Store
	history History
	ranker  Ranker
	root    string
	metrics *telemetry.Metrics
	flights singleflight.Group
	mu      sync.Mutex
	cache   map[string]pool
	limits  map[string]userLimit
}

func New(ctx context.Context, lib *library.Library, store Store, history History, ranker Ranker, metrics *telemetry.Metrics) *Service {
	root, _ := lib.SourceRoot()
	return &Service{ctx: ctx, lib: lib, store: store, history: history, ranker: ranker, root: root, metrics: metrics, cache: map[string]pool{}, limits: map[string]userLimit{}}
}
func normalize(in Request) (Request, string, error) {
	if !requestID.MatchString(in.RequestID) || len(in.Cursor) > 128 || len(in.SeedTrackID) > 256 || in.PageSize < 0 || in.PageSize > MaxPageSize || len(in.QueuedTrackIDs) > MaxExclusions || len(in.ExcludedTrackIDs) > MaxExclusions {
		return in, "", ErrInvalid
	}
	// Preserve canonical request hashes from earlier releases for safe retries.
	// The response batch is capped separately when generating a new page.
	if in.PageSize == 0 {
		in.PageSize = 10
	}
	for _, ids := range [][]string{in.QueuedTrackIDs, in.ExcludedTrackIDs} {
		for _, id := range ids {
			if id == "" || len(id) > 256 {
				return in, "", ErrInvalid
			}
		}
	}
	in.QueuedTrackIDs = append([]string{}, in.QueuedTrackIDs...)
	in.ExcludedTrackIDs = append([]string{}, in.ExcludedTrackIDs...)
	sort.Strings(in.ExcludedTrackIDs)
	in.ExcludedTrackIDs = slices.Compact(in.ExcludedTrackIDs)
	copy := in
	copy.RequestID = ""
	raw, _ := json.Marshal(copy)
	return in, fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}
func (s *Service) allow(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.limits[owner]
	if !ok {
		if len(s.limits) >= 1024 {
			var oldest string
			var at time.Time
			for key, v := range s.limits {
				if oldest == "" || v.at.Before(at) {
					oldest, at = key, v.at
				}
			}
			delete(s.limits, oldest)
		}
		entry = userLimit{limiter: rate.NewLimiter(rate.Every(time.Second), 4)}
	}
	entry.at = time.Now()
	s.limits[owner] = entry
	return entry.limiter.Allow()
}
func (s *Service) Get(ctx context.Context, owner string, input Request) (Page, error) {
	in, hash, err := normalize(input)
	if err != nil || owner == "" {
		return Page{}, ErrInvalid
	}
	if saved, err := s.store.Request(ctx, owner, s.root, in.RequestID); err == nil {
		if saved.Hash != hash {
			return Page{}, ErrConflict
		}
		return s.hydrate(ctx, saved.Page)
	} else if !errors.Is(err, ErrNotFound) {
		return Page{}, err
	}
	key := owner + "/request/" + in.RequestID
	result := s.flights.DoChan(key, func() (any, error) {
		// Bounded shared work survives a caller's network retry but stops with the app.
		work, cancel := context.WithTimeout(s.ctx, 12*time.Second)
		defer cancel()
		if saved, err := s.store.Request(work, owner, s.root, in.RequestID); err == nil {
			if saved.Hash != hash {
				return nil, ErrConflict
			}
			return saved, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if !s.allow(owner) {
			return nil, ErrCapacity
		}
		start := time.Now()
		page, source, err := s.generate(work, owner, in)
		s.metrics.PlayFeed(source, len(page.Items), err, time.Since(start))
		if err != nil {
			return nil, err
		}
		return s.store.Save(work, owner, s.root, in, hash, page)
	})
	select {
	case <-ctx.Done():
		return Page{}, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return Page{}, result.Err
		}
		receipt := result.Val.(Receipt)
		if receipt.Hash != hash {
			return Page{}, ErrConflict
		}
		return s.hydrate(ctx, receipt.Page)
	}
}
func (s *Service) hydrate(ctx context.Context, page Page) (Page, error) {
	current := make([]Item, 0, len(page.Items))
	for _, item := range page.Items {
		track, err := s.lib.Get(ctx, item.Track.ID)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return Page{}, err
		}
		item.Track = track
		current = append(current, item)
	}
	page.Items = current
	return page, nil
}
func (s *Service) generate(ctx context.Context, owner string, in Request) (Page, string, error) {
	page := Page{FeedID: rand.Text(), Items: []Item{}}
	in.PageSize = min(in.PageSize, 3)
	excluded := map[string]bool{}
	for _, ids := range [][]string{in.QueuedTrackIDs, in.ExcludedTrackIDs} {
		for _, id := range ids {
			excluded[id] = true
		}
	}
	if in.Cursor != "" {
		next, err := s.store.Continue(ctx, owner, s.root, in.Cursor)
		if err != nil {
			return page, "error", err
		}
		if next.Issued != nil {
			return *next.Issued, "replay", nil
		}
		page.FeedID = next.FeedID
		for _, id := range next.Seen {
			excluded[id] = true
		}
	}
	var state Context
	if in.SeedTrackID != "" {
		seed, err := s.lib.Get(ctx, in.SeedTrackID)
		if err != nil {
			return page, "error", err
		}
		seed.Path = ""
		state.Seed = seed
		excluded[seed.ID] = true
	}
	// The most recent future manual choices provide context without sending queues,
	// account identities, access URLs, or source filesystem paths to the ranker.
	for _, id := range in.QueuedTrackIDs[max(0, len(in.QueuedTrackIDs)-8):] {
		t, err := s.lib.Get(ctx, id)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return page, "error", err
		}
		t.Path = ""
		state.Upcoming = append(state.Upcoming, t)
	}
	history, err := s.history.List(ctx, owner, s.root, listening.Query{Limit: 20})
	if err != nil {
		return page, "error", err
	}
	// Recent event detail controls immediate repeat/skip exclusions. The model's
	// long-term preferences include the listener's entire recorded history.
	preferences(history.Sessions, time.Now(), excluded)
	state.Preferences, err = s.history.Preferences(ctx, owner, s.root)
	if err != nil {
		return page, "error", err
	}
	state.AsOf = time.Now().UTC()
	for _, session := range history.Sessions[:min(20, len(history.Sessions))] {
		track := session.Track
		track.Path = ""
		state.Recent = append(state.Recent, RecentListen{Track: track, StartedAt: session.StartedAt, UpdatedAt: session.UpdatedAt, EndedAt: session.EndedAt, ListenedMS: session.ListenedMS, PositionMS: session.PositionMS, Playing: session.Playing, Outcome: session.Outcome, ChangeReason: session.ChangeReason})
	}
	ids := sortedIDs(excluded)
	available, err := s.store.Count(ctx, s.root, ids)
	if err != nil {
		return page, "error", err
	}
	if available == 0 {
		page.Exhausted = true
		return page, "empty", nil
	}
	revision, err := s.lib.Revision(ctx)
	if err != nil {
		return page, "error", err
	}
	raw, _ := json.Marshal(struct {
		Owner     string
		RequestID string
		Revision  int64
		State     Context
	}{owner, in.RequestID, revision, state})
	key := fmt.Sprintf("%x", sha256.Sum256(raw))
	candidates, err := s.candidates(ctx, key, state, nil)
	if err != nil {
		return page, "error", err
	}
	usable := pool{tracks: []library.Track{}, scores: candidates.scores, source: candidates.source}
	for _, t := range candidates.tracks {
		if !excluded[t.ID] {
			usable.tracks = append(usable.tracks, t)
		}
	}
	// A large manual queue or a later page may exclude the cached pool. Refill from
	// the remaining catalog so pagination can reach the entire available library.
	if len(usable.tracks) < min(in.PageSize, available) {
		exclusions := append([]string{}, ids...)
		for _, t := range candidates.tracks {
			exclusions = append(exclusions, t.ID)
		}
		extraRaw, _ := json.Marshal(exclusions)
		extraKey := key + fmt.Sprintf("%x", sha256.Sum256(extraRaw))
		extra, err := s.candidates(ctx, extraKey, state, exclusions)
		if err != nil {
			return page, "error", err
		}
		usable.tracks = append(usable.tracks, extra.tracks...)
		scores := map[string]float64{}
		for id, score := range usable.scores {
			scores[id] = score
		}
		for id, score := range extra.scores {
			scores[id] = score
		}
		usable.scores = scores
		if extra.source == "fallback" {
			usable.source = "fallback"
		}
	}
	anchor := state.Seed.Artist
	if len(state.Upcoming) > 0 {
		anchor = state.Upcoming[len(state.Upcoming)-1].Artist
	}
	selected := selectExploring(state, usable.tracks, usable.scores, in.PageSize, anchor)
	for _, track := range selected {
		page.Items = append(page.Items, Item{ID: rand.Text(), Track: track})
	}
	page.Exhausted = available <= len(selected)
	if !page.Exhausted {
		page.NextCursor = rand.Text()
	}
	return page, usable.source, nil
}
func sortedIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func preferences(sessions []listening.Session, now time.Time, excluded map[string]bool) []Preference {
	byID := map[string]*Preference{}
	for _, session := range sessions {
		if session.Outcome == "" || session.Outcome == "stopped" {
			continue
		}
		p := byID[session.Track.ID]
		if p == nil {
			t := session.Track
			t.Path = ""
			p = &Preference{Track: t}
			byID[t.ID] = p
		}
		at := session.StartedAt
		if session.EndedAt.After(at) {
			at = session.EndedAt
		}
		p.ListenedMS += session.ListenedMS
		if at.After(p.LastPlayedAt) {
			p.LastPlayedAt = at
		}
		fraction := 0.0
		if session.Track.Duration > 0 {
			fraction = float64(session.ListenedMS) / (session.Track.Duration * 1000)
		}
		switch {
		case session.Outcome == "completed" || fraction >= .8:
			p.Completed++
		case session.Outcome == "skipped" && session.ChangeReason == "next" && session.ListenedMS < 10000 && (session.Track.Duration <= 0 || fraction < .1):
			p.EarlySkips++
			if now.Sub(at) < 2*time.Hour {
				excluded[session.Track.ID] = true
			}
		case session.Outcome == "skipped":
			p.OtherSkips++
		}
		// Recent actual playback should not immediately loop back into autoplay.
		if now.Sub(at) < 15*time.Minute && session.ListenedMS > 0 {
			excluded[session.Track.ID] = true
		}
	}
	out := make([]Preference, 0, len(byID))
	for _, p := range byID {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastPlayedAt.After(out[j].LastPlayedAt) })
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}
func favoriteArtists(state Context) []string {
	counts := map[string]int{}
	if state.Seed.Artist != "" {
		counts[state.Seed.Artist] += 10
	}
	for _, t := range state.Upcoming {
		if t.Artist != "" {
			counts[t.Artist] += 3
		}
	}
	for _, p := range state.Preferences {
		if p.Completed > 0 && p.Track.Artist != "" {
			counts[p.Track.Artist] += p.Completed
		}
	}
	out := make([]string, 0, len(counts))
	for artist := range counts {
		out = append(out, artist)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] == counts[out[j]] {
			return out[i] < out[j]
		}
		return counts[out[i]] > counts[out[j]]
	})
	return out[:min(10, len(out))]
}
func (s *Service) candidates(ctx context.Context, key string, state Context, excluded []string) (pool, error) {
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Since(cached.at) < time.Minute {
		return cached, nil
	}
	result, err, _ := s.flights.Do("rank/"+key, func() (any, error) {
		s.mu.Lock()
		cached, ok := s.cache[key]
		s.mu.Unlock()
		if ok && time.Since(cached.at) < time.Minute {
			return cached, nil
		}
		artists := favoriteArtists(state)
		// Until listening has substantial breadth, never reserve most candidates
		// for artists inferred from a tiny sample or the current queue.
		if ExplorationFor(state).Confidence < .5 {
			artists = nil
		}
		tracks, err := s.store.Candidates(ctx, s.root, artists, excluded, MaxCandidates)
		if err != nil {
			return pool{}, err
		}
		scores := map[string]float64{}
		for _, t := range tracks {
			scores[t.ID] = fallbackScore(state, t)
		}
		source := "fallback"
		if s.ranker != nil && len(tracks) > 0 {
			ranked, err := s.ranker.Recommend(ctx, state, tracks)
			if ctx.Err() != nil {
				return pool{}, ctx.Err()
			}
			if err == nil && validScores(ranked, tracks) {
				for _, rank := range ranked {
					scores[rank.TrackID] = rank.Value
				}
				source = "jev"
			}
		}
		cached = pool{tracks: tracks, scores: scores, source: source, at: time.Now()}
		s.mu.Lock()
		if len(s.cache) >= 128 {
			var oldest string
			var at time.Time
			for k, v := range s.cache {
				if oldest == "" || v.at.Before(at) {
					oldest, at = k, v.at
				}
			}
			delete(s.cache, oldest)
		}
		s.cache[key] = cached
		s.mu.Unlock()
		return cached, nil
	})
	if err != nil {
		return pool{}, err
	}
	return result.(pool), nil
}
func validScores(scores []Score, tracks []library.Track) bool {
	if len(scores) != len(tracks) {
		return false
	}
	allowed := map[string]bool{}
	for _, t := range tracks {
		allowed[t.ID] = true
	}
	for _, score := range scores {
		if !allowed[score.TrackID] || math.IsNaN(score.Value) || math.IsInf(score.Value, 0) || score.Value < 0 || score.Value > 1 {
			return false
		}
		delete(allowed, score.TrackID)
	}
	return len(allowed) == 0
}
func fallbackScore(state Context, t library.Track) float64 {
	score := .4
	if strings.EqualFold(t.Genre, state.Seed.Genre) && t.Genre != "" {
		score += .15
	}
	if strings.EqualFold(t.Artist, state.Seed.Artist) && t.Artist != "" {
		score += .15
	}
	for _, p := range state.Preferences {
		if t.ID == p.Track.ID {
			score += math.Min(.2, float64(p.Completed)*.04)
			score -= math.Min(.35, float64(p.EarlySkips)*.15)
			score -= math.Min(.1, float64(p.OtherSkips)*.02)
		}
		if t.Artist == p.Track.Artist && p.Completed > 0 {
			score += .02
		}
	}
	return math.Max(0, math.Min(1, score))
}
