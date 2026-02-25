// CLAUDE:SUMMARY Re-exports internal store types (Profile, Correction, Report, InstanceReputation, LeaderboardEntry) for external callers.
package domregistry

import "github.com/hazyhaar/chrc/domregistry/internal/store"

// Re-exported types from internal/store for use by cmd/ and external callers.
type (
	Profile            = store.Profile
	Correction         = store.Correction
	Report             = store.Report
	InstanceReputation = store.InstanceReputation
	LeaderboardEntry   = store.LeaderboardEntry
)
