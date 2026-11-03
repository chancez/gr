package review

import (
	"context"
	"errors"

	"github.com/chancez/gr/internal/store"
)

// SavePlace records where the author was in a change, as the TUI describes it.
func (s *Service) SavePlace(ctx context.Context, change store.Change, place []byte) error {
	return s.store.SavePlace(ctx, change.ID, s.author, place)
}

// Place is where the author was in a change, and whether one was saved.
func (s *Service) Place(ctx context.Context, change store.Change) ([]byte, bool, error) {
	place, err := s.store.Place(ctx, change.ID, s.author)
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	return place, err == nil, err
}
