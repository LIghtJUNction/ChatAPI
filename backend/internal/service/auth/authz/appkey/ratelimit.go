package app

import "time"

func (s *Service) AllowRequest(principal Principal, now time.Time) bool {
	limit := principal.MaxRequestsPerMinute
	if limit <= 0 {
		return true
	}
	keyID := principal.KeyID
	if keyID == "" {
		return false
	}
	cutoff := now.Add(-time.Minute)
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	hits := s.rateLimitHits[keyID]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= limit {
		s.rateLimitHits[keyID] = kept
		return false
	}
	kept = append(kept, now)
	s.rateLimitHits[keyID] = kept
	return true
}

func (s *Service) AllowRequestNow(principal Principal) bool {
	return s.AllowRequest(principal, time.Now().UTC())
}
