// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !race

package wasm_test

// raceEnabled mirrors the standard library's own internal/race.Enabled, which is not importable
// from outside std - this pair of build-tagged files is the conventional way external packages get
// the same answer, as internal/config does for its own budget test. TestPluginRuntimePiClassBudget
// needs it: under -race the instrumentation dominates every figure it reports (measured directly:
// 4.2 s cold compile against 0.5 s without it, 14.7 ms warm invocation against 3.3 ms), so the
// budgets would be asserting the detector's overhead rather than the runtime's cost.
const raceEnabled = false
