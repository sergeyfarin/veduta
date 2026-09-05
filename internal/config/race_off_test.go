// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !race

package config_test

// raceEnabled mirrors the standard library's own internal/race.Enabled, which is not importable
// from outside std - this pair of build-tagged files is the conventional way external packages
// get the same answer. TestLoad_500CardsUnder50ms needs it: the race detector's instrumentation
// overhead (measured directly: ~180ms here, against ~22ms without it) has nothing to do with
// this package's own cost, and asserting a real wall-clock budget under it would be asserting
// something else entirely.
const raceEnabled = false
