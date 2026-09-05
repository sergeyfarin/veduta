// Command veduta serves the dashboard.
//
// Milestone A1: build identity only. The HTTP server arrives in A3, and until H1 lands it will
// refuse to bind anything but loopback (see docs/01-architecture.md D46).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"veduta.dev/veduta/internal/version"
)

func main() {
	asJSON := flag.Bool("json", false, "print build identity as JSON")
	flag.Parse()

	info := version.Current()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(info); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Printf("veduta %s (%s)\nsource: %s\n", info.Version, info.Commit, info.SourceURL)
}
