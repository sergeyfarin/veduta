// SPDX-License-Identifier: AGPL-3.0-or-later

package config

// Snapshot is the immutable result of a successful Load: a schema-valid, semantically-checked
// Config plus the small indices its own validation already had to build. Callers must treat it
// as read-only - the whole point of atomic.Pointer[Snapshot] (docs/01-architecture.md section 2)
// is that readers never lock, which only holds if nobody mutates a Snapshot after it is published.
type Snapshot struct {
	Config Config

	// SecretRefs is every ${secret:NAME} occurrence found while loading, in file order - see
	// SecretLocation. internal/secrets (milestone C2) resolves each name once and reports a
	// diagnostic per occurrence for any that fails, so a missing secret used in three places is
	// three real locations, not one anonymous complaint.
	SecretRefs []SecretLocation

	cardsByID        map[string]*Card
	integrationsByID map[string]*Integration
}

func newSnapshot(cfg Config, secretRefs []SecretLocation) *Snapshot {
	s := &Snapshot{
		Config:           cfg,
		SecretRefs:       secretRefs,
		cardsByID:        make(map[string]*Card),
		integrationsByID: make(map[string]*Integration, len(cfg.Integrations)),
	}
	for si := range s.Config.Sections {
		cards := s.Config.Sections[si].Cards
		for ci := range cards {
			s.cardsByID[cards[ci].ID] = &cards[ci]
		}
	}
	for i := range s.Config.Integrations {
		s.integrationsByID[s.Config.Integrations[i].ID] = &s.Config.Integrations[i]
	}
	return s
}

// CardByID looks up a card by id in O(1) - built once at Load time, since validateSemantics
// already walks every card to check for duplicates and dangling references.
func (s *Snapshot) CardByID(id string) (*Card, bool) {
	c, ok := s.cardsByID[id]
	return c, ok
}

// IntegrationByID looks up a declared integration by id in O(1).
func (s *Snapshot) IntegrationByID(id string) (*Integration, bool) {
	in, ok := s.integrationsByID[id]
	return in, ok
}
