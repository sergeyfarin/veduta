// SPDX-License-Identifier: AGPL-3.0-or-later

// Milestone D2b: `veduta integration list|diff|approve` - the CLI side of the two-step,
// digest-bound approval transaction docs/01-architecture.md section 6 describes, and the one
// path available regardless of auth mode (forward-auth and `auth: none` are documented as
// CLI-only for approval). It talks to internal/integrations directly rather than over HTTP, so
// an administrator can approve an integration before the server is even running.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/integrations"
)

func integrationCmd(args []string) error {
	usage := errors.New("usage: veduta integration list|diff|approve [--config path]")
	if len(args) == 0 {
		return usage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return integrationList(rest)
	case "diff":
		return integrationDiff(rest)
	case "approve":
		return integrationApprove(rest)
	default:
		return usage
	}
}

// integrationSet is the loaded config, resolved lock path and lock document shared by every
// integration subcommand - each one loads the same two files, so this is done once per command.
type integrationSet struct {
	snapshot *config.Snapshot
	dir      string // primary config file's directory - what `path:` sources resolve against
	lockPath string
	lock     *integrations.Lock
}

func loadIntegrationSet(configPath string) (*integrationSet, error) {
	snapshot, diags := config.LoadPath(configPath)
	if snapshot == nil || diags.HasErrors() {
		return nil, fmt.Errorf("load config:\n%s", diags.String())
	}
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, integrations.LockFileName)
	lock, err := integrations.ReadLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", lockPath, err)
	}
	return &integrationSet{snapshot: snapshot, dir: dir, lockPath: lockPath, lock: lock}, nil
}

func integrationList(args []string) error {
	fs := flag.NewFlagSet("integration list", flag.ContinueOnError)
	configPath := fs.String("config", "veduta.yaml", "path to the primary config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := loadIntegrationSet(*configPath)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(set.snapshot.Config.Integrations))
	for _, in := range set.snapshot.Config.Integrations {
		ids = append(ids, in.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		in, _ := set.snapshot.IntegrationByID(id)
		src, err := integrations.ResolveSource(set.dir, in.Source)
		if err != nil {
			fmt.Printf("%-16s error: %v\n", id, err)
			continue
		}
		if src.Builtin {
			fmt.Printf("%-16s %-11s (ships in the binary, no separate trust boundary)\n", id, integrations.StatusBuiltin)
			continue
		}
		m, err := integrations.LoadManifest(src.Dir)
		if err != nil {
			fmt.Printf("%-16s error: %v\n", id, err)
			continue
		}
		status := integrations.Evaluate(m, set.lock.Integrations[id])
		fmt.Printf("%-16s %-11s runtime=%-11s version=%-8s capabilities=%s\n",
			id, status, m.Runtime, m.Version, strings.Join(m.Capabilities, ","))
	}
	return nil
}

// resolveManifest loads config and the lock, then resolves and loads one named integration's
// manifest - the shared first half of both `diff` and `approve`.
func resolveManifest(configPath, id string) (*integrationSet, *integrations.Manifest, error) {
	set, err := loadIntegrationSet(configPath)
	if err != nil {
		return nil, nil, err
	}
	in, ok := set.snapshot.IntegrationByID(id)
	if !ok {
		return nil, nil, fmt.Errorf("no integration %q declared in %s", id, configPath)
	}
	src, err := integrations.ResolveSource(set.dir, in.Source)
	if err != nil {
		return nil, nil, err
	}
	if src.Builtin {
		return nil, nil, fmt.Errorf("%q is a builtin integration - exempt from the lock, nothing to approve", id)
	}
	m, err := integrations.LoadManifest(src.Dir)
	if err != nil {
		return nil, nil, err
	}
	return set, m, nil
}

func integrationDiff(args []string) error {
	fs := flag.NewFlagSet("integration diff", flag.ContinueOnError)
	configPath := fs.String("config", "veduta.yaml", "path to the primary config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: veduta integration diff <id> [--config path]")
	}
	id := fs.Arg(0)
	set, m, err := resolveManifest(*configPath, id)
	if err != nil {
		return err
	}
	entry := set.lock.Integrations[id]
	diff, err := integrations.ComputeDiff(m, entry)
	if err != nil {
		return err
	}
	printDiff(os.Stdout, id, m, diff, integrations.Evaluate(m, entry))
	return nil
}

// printDiff's opening line depends on status, not merely on whether diff is empty: an approved
// integration can still show a non-empty diff when it was deliberately approved for a subset of
// what its manifest asks (docs/01-architecture.md: "approving a subset ... is the normal case,
// not an exception") - that is a different situation from a changed manifest needing
// re-approval, and conflating the two would misreport a normal, intentional state as an alarm.
func printDiff(w io.Writer, id string, m *integrations.Manifest, diff integrations.Diff, status integrations.Status) {
	if diff.Empty() {
		_, _ = fmt.Fprintf(w, "%s: manifest digest %s - already approved, nothing to review\n", id, m.Digest)
		return
	}
	switch status {
	case integrations.StatusUnapproved:
		_, _ = fmt.Fprintf(w, "%s: not yet approved. Manifest digest %s requests:\n", id, m.Digest)
	case integrations.StatusChanged:
		_, _ = fmt.Fprintf(w, "%s: manifest changed (now digests to %s). Permission diff:\n", id, m.Digest)
	default:
		_, _ = fmt.Fprintf(w, "%s: approved, but the manifest requests more than what is currently granted:\n", id)
	}
	for _, c := range diff.AddedCapabilities {
		_, _ = fmt.Fprintf(w, "  + capability %s\n", c)
	}
	for _, r := range diff.AddedRoutes {
		_, _ = fmt.Fprintf(w, "  + route %s %s %s\n", r.Method, r.Path, routeReason(r))
	}
	for _, r := range diff.RemovedRoutes {
		_, _ = fmt.Fprintf(w, "  - route %s %s (no longer requested)\n", r.Method, r.Path)
	}
	for _, l := range diff.RaisedLimits {
		_, _ = fmt.Fprintf(w, "  ~ limit %s: %d -> %d\n", l.Field, l.Current, l.Requested)
	}
	for _, r := range diff.BodyBearingRoutes {
		_, _ = fmt.Fprintf(w, "  ! %s %s carries a request body: approving it approves whatever the body selects\n", r.Method, r.Path)
	}
}

func routeReason(r integrations.Route) string {
	if r.Reason == "" {
		return ""
	}
	return "(" + r.Reason + ")"
}

func integrationApprove(args []string) error {
	fs := flag.NewFlagSet("integration approve", flag.ContinueOnError)
	configPath := fs.String("config", "veduta.yaml", "path to the primary config file")
	by := fs.String("by", currentUsername(), "recorded as approvedBy")
	yes := fs.Bool("yes", false, "approve without an interactive confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: veduta integration approve <id> [--config path] [--by name] [--yes]")
	}
	id := fs.Arg(0)
	set, m, err := resolveManifest(*configPath, id)
	if err != nil {
		return err
	}
	entry := set.lock.Integrations[id]
	diff, err := integrations.ComputeDiff(m, entry)
	if err != nil {
		return err
	}
	printDiff(os.Stdout, id, m, diff, integrations.Evaluate(m, entry))
	if diff.Empty() {
		return nil
	}
	if !*yes && !confirm(os.Stdin, os.Stdout) {
		return errors.New("not approved")
	}

	// The CLI always approves the manifest's full current request - there is no interactive
	// picker for a narrower subset here (the REST endpoint exists for that; see
	// docs/01-architecture.md's "a client may approve a strict subset of the requested grants").
	newEntry, err := integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       m.Routes,
		Limits:       m.Limits,
	}, *by, time.Now())
	if err != nil {
		return err
	}
	set.lock.Integrations[id] = newEntry
	if err := integrations.WriteLock(set.lockPath, set.lock); err != nil {
		return err
	}
	fmt.Printf("%s: approved, %s updated\n", id, set.lockPath)
	return nil
}

func confirm(in io.Reader, out io.Writer) bool {
	_, _ = fmt.Fprint(out, "Approve? [y/N]: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func currentUsername() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return ""
	}
	return u.Username
}
