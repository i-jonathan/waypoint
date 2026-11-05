package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"waypoint/internal/domain"
)

// SettingsHash fingerprints the settings a settings editor shows: the hex
// sha256 of their JSON, with NextID left out so that adding a card does not
// invalidate an editor that is open. An empty list and a missing one hash alike.
// Pass it back to UpdateSettings as ifMatch to refuse a change made against
// settings that have since moved on.
func SettingsHash(s domain.Settings) string {
	s = normalizeSettings(s)
	s.NextID = 0
	data, err := json.Marshal(s)
	if err != nil { // a struct of strings and numbers cannot fail to encode
		panic(fmt.Sprintf("encode settings: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// normalizeSettings copies s with every list non-nil, so it marshals to [] and
// never null, and shares nothing with s.
func normalizeSettings(s domain.Settings) domain.Settings {
	s.Columns = append([]domain.Column{}, s.Columns...)
	for i := range s.Columns {
		s.Columns[i].MovesTo = append([]string{}, s.Columns[i].MovesTo...)
	}
	s.Groups = append([]domain.Group{}, s.Groups...)
	s.Priorities = append([]string{}, s.Priorities...)
	return s
}

// SettingsConflictError reports that the settings changed since the caller read
// them. Current is the board as it is now.
type SettingsConflictError struct{ Current BoardView }

func (e *SettingsConflictError) Error() string {
	return fmt.Sprintf("%s: the board's settings changed since they were read", domain.ErrConflict)
}

// Unwrap makes errors.Is(err, domain.ErrConflict) true.
func (e *SettingsConflictError) Unwrap() error { return domain.ErrConflict }
