// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
	wasmrt "veduta.dev/veduta/internal/integrations/wasm"
)

// The S1b kill criteria, written before any of this existed: a warm invocation over 50 ms, or an
// instance costing more than 20 MB resident, would have meant abandoning in-process WASM for a
// sidecar. They are the thresholds these measurements are judged against, unchanged.
const (
	warmInvokeBudget     = 50 * time.Millisecond
	rssPerInstanceBudget = 20 << 20
)

// Cold compilation has no kill criterion attached to it: it happens once per module per cache
// generation, and a slow one delays a start rather than a refresh. It still gets a ceiling,
// because "once" is also "on every container start with an empty cache volume", and an unbounded
// number would make a Pi-class boot indefensible. Five seconds matches the load gate's budget.
const coldCompileBudget = 5 * time.Second

// marginalInstances is how many further instances the resident-memory figure is averaged over.
// One instance cannot separate the runtime's fixed cost - wazero, its compiler, the Go heap that
// hosts them - from what each additional integration adds, and the fixed cost is paid whether a
// host runs one plugin or ten.
const marginalInstances = 7

// warmInvocations is the sample size for warm invocation, of which the first few are discarded.
const warmInvocations = 24

// TestPluginRuntimePiClassBudget measures the three figures S1a was written to produce - cold
// compilation of a real plugin, warm invocation, and resident memory per loaded instance - using
// plugins/jellyfin/jellyfin.wasm and the fixtures the golden test already pins.
//
// It is architecture-neutral and runs everywhere, but it exists for the native ARM64 CI job: run
// there, its numbers are the hardware evidence G1's acceptance asked for and cross-compilation
// could never supply. Run on an x86 developer machine it is a regression test with weaker meaning,
// since the budgets are ARM-shaped. armv7 is not covered by it: no native 32-bit ARM runner exists
// to run it on, and emulation would measure the emulator.
//
// Every measurement is logged whether or not it passes, so the CI log is the record of the numbers
// rather than merely of the verdict. See docs/03-backlog.md for what they were outstanding for.
func TestPluginRuntimePiClassBudget(t *testing.T) {
	// The gate's own CI job runs without -race for exactly this reason; the repository-wide
	// `go test -race ./...` still compiles and type-checks this file, it simply does not time it.
	if raceEnabled {
		t.Skip("skipped under -race: instrumentation overhead swamps every budget being measured")
	}
	// Four execution threads model a Raspberry Pi 4 class CPU, as the scheduler's load gate does.
	// Compilation is the phase that uses them: wazero compiles a module's functions in parallel.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))

	ctx := context.Background()
	broker := &jellyfinBroker{t: t, dataDir: filepath.Join(jellyfinPluginDir, "testdata")}
	installed, grant := approvedJellyfin(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")

	// Cold: an empty compilation cache, so the module is read, digest-verified, preflighted for
	// forbidden imports and compiled to machine code from nothing - what a first run after an
	// install or an image pull pays, before any card renders.
	cold := newRuntime(t, ctx, cacheDir, broker)
	started := time.Now()
	instance, err := cold.Load(ctx, installed)
	coldCompile := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}

	// Warm compile: a second runtime over the same cache directory, so wazero recovers machine
	// code from the cache instead of compiling it. This is what a restart costs, and it is the
	// number that decides whether the cache directory is worth persisting on a Pi-class host.
	cached := newRuntime(t, ctx, cacheDir, broker)
	started = time.Now()
	if _, err = cached.Load(ctx, installed); err != nil {
		t.Fatal(err)
	}
	cachedCompile := time.Since(started)

	// Warm invocation: the per-call path, which instantiates the compiled module, runs the guest,
	// serves its four brokered HTTP calls from fixtures and validates the returned document. The
	// first calls are discarded - they fault in pages the steady state does not pay for.
	samples := make([]time.Duration, 0, warmInvocations)
	for i := range warmInvocations {
		started = time.Now()
		response, invokeErr := instance.Invoke(ctx, integrations.InvokeRequest{
			Operation: "recently-added", Params: json.RawMessage(`{"limit":5}`), Grant: grant,
		})
		elapsed := time.Since(started)
		if invokeErr != nil {
			t.Fatalf("invocation %d: %v", i, invokeErr)
		}
		// A failed or empty invocation would be fast and meaningless; the measurement is only
		// worth anything if each call did the plugin's whole job.
		if len(response.Document.Blocks) == 0 {
			t.Fatalf("invocation %d produced an empty document", i)
		}
		if i >= warmInvocations/4 {
			samples = append(samples, elapsed)
		}
	}
	slices.Sort(samples)
	median, worst := samples[len(samples)/2], samples[len(samples)-1]

	// Resident memory per instance. The baseline is taken after one instance has been loaded and
	// invoked, so the runtime's one-off cost is already resident and what follows is marginal.
	// Every sample follows a forced return of free heap to the OS, so the delta is memory the
	// process is holding rather than garbage it has not yet handed back.
	baseline, ok := residentBytes()
	if !ok {
		t.Log("resident memory: /proc unavailable on this platform, per-instance figure skipped")
	}
	for i := range marginalInstances {
		extra, loadErr := cold.Load(ctx, installed)
		if loadErr != nil {
			t.Fatalf("instance %d: %v", i+1, loadErr)
		}
		// Instances are measured in use, not merely loaded: an instance that has never run has
		// not yet paid for the per-call instantiation its memory limit is sized for.
		if _, invokeErr := extra.Invoke(ctx, integrations.InvokeRequest{
			Operation: "recently-added", Params: json.RawMessage(`{"limit":5}`), Grant: grant,
		}); invokeErr != nil {
			t.Fatalf("instance %d invocation: %v", i+1, invokeErr)
		}
	}
	loaded, _ := residentBytes()
	perInstance := (loaded - baseline) / marginalInstances

	t.Logf("cold compile:        %v (budget %v)", coldCompile.Round(time.Millisecond), coldCompileBudget)
	t.Logf("compile from cache:  %v", cachedCompile.Round(time.Millisecond))
	t.Logf("warm invoke median:  %v (budget %v)", median.Round(time.Microsecond), warmInvokeBudget)
	t.Logf("warm invoke worst:   %v over %d samples", worst.Round(time.Microsecond), len(samples))
	if ok {
		t.Logf("resident per instance: %s over %d instances (budget %s)",
			mib(perInstance), marginalInstances, mib(rssPerInstanceBudget))
		t.Logf("process peak resident: %s", mib(peakResidentBytes()))
	}
	t.Logf("GOARCH=%s GOOS=%s NumCPU=%d", runtime.GOARCH, runtime.GOOS, runtime.NumCPU())

	if coldCompile > coldCompileBudget {
		t.Errorf("cold compile %v exceeds %v", coldCompile, coldCompileBudget)
	}
	// The median carries the gate and the worst case is reported beside it. A single sample can be
	// lost to a GC pause or a noisy neighbour on a shared runner, and a gate that fails on one
	// outlier gets disabled by the third false alarm, which is worse than no gate.
	if median > warmInvokeBudget {
		t.Errorf("warm invocation median %v exceeds the S1b criterion of %v", median, warmInvokeBudget)
	}
	if ok && perInstance > rssPerInstanceBudget {
		t.Errorf("resident memory per instance %s exceeds the S1b criterion of %s",
			mib(perInstance), mib(rssPerInstanceBudget))
	}
}

// approvedJellyfin builds the installed plugin and the grant its operation runs under, with the
// same approved limits the golden test uses - measuring a differently-limited plugin would measure
// a configuration nobody ships.
func approvedJellyfin(t *testing.T) (integrations.Installed, capabilities.Grant) {
	t.Helper()
	manifest, err := manifestload.Load(filepath.Join(jellyfinPluginDir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	limits := integrations.EffectiveLimits{MemoryMB: 64, TimeoutMs: 3000, OutputKB: 64, HTTPRequests: 4, ResponseMB: 4, CacheEntries: 64, InputMB: 4, JSONDepth: 32, JSONNodes: 200000, ExprNodes: 512, Iterations: 20000, RequestBodyKB: 64, HostCalls: 10, CacheBytesKB: 256}
	routes := manifestRoutes(manifest)
	lock := &integrations.LockEntry{ManifestSHA256: manifest.Digest, ModuleSHA256: manifest.ModuleSHA256, Version: manifest.Version, Runtime: "wasm", Capabilities: []string{"http", "assets"}, Routes: lockRoutes(routes), EffectiveLimits: limits}
	grant := capabilities.NewGrant("jellyfin", "1.0.0", "jellyfin-recent", map[string]string{"server": "jellyfin"}, capabilities.NewCapSet("http", "assets"), routes, routes, nil, capabilities.Limits{HTTPRequests: 4, ResponseMB: 4, HostCalls: 10}, capabilities.ExecutionIdentity{})
	return integrations.Installed{Manifest: manifest, Lock: lock}, grant
}

func newRuntime(t *testing.T, ctx context.Context, cacheDir string, broker capabilities.Broker) *wasmrt.Runtime {
	t.Helper()
	rt, err := wasmrt.NewWithBroker(ctx, cacheDir, broker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

// residentBytes reports the process's current resident set from /proc, after returning free heap
// to the OS so the figure reflects retained memory. Go's own heap statistics would miss the point
// here: wazero's compiled machine code is mapped, not heap-allocated, and it is most of the cost.
// Reported ok=false anywhere /proc is not available, which is every non-Linux developer machine
// and none of CI.
func residentBytes() (int64, bool) {
	debug.FreeOSMemory()
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * int64(os.Getpagesize()), true
}

// peakResidentBytes reports the kernel's high-water mark for the whole process, which includes the
// test binary and Go runtime as well as the plugin runtime. It is logged for context, never gated:
// it answers "did anything transiently balloon", not "what does an instance cost".
func peakResidentBytes() int64 {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb << 10
	}
	return 0
}

func mib(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/(1<<20), 'f', 1, 64) + " MiB"
}
