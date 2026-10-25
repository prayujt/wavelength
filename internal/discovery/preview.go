package discovery

const previewLimit = 4096

// Preview is metadata already seen during discovery, without another provider
// request. Jobs may use it for display while authoritative resolution is pending.
func (s *Service) Preview(ref Ref) *Track {
	if s == nil {
		return nil
	}
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	track, ok := s.previews[ref]
	if !ok {
		return nil
	}
	return &track
}

func (s *Service) remember(tracks []Track) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	for _, track := range tracks {
		if !track.Ref.ValidMetadata() || track.Title == "" || track.Artist == "" {
			continue
		}
		if s.previews == nil || len(s.previews) >= previewLimit {
			s.previews = make(map[Ref]Track)
		}
		s.previews[track.Ref] = track
	}
}
