// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"veduta.dev/veduta/internal/integrations/wasm"
)

func pluginCmd(args []string) error {
	usage := errors.New("usage: veduta plugin validate <file.wasm>")
	if len(args) == 0 || args[0] != "validate" {
		return usage
	}
	flags := flag.NewFlagSet("plugin validate", flag.ContinueOnError)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return usage
	}
	path := flags.Arg(0)
	if err := wasm.ValidateFile(context.Background(), path); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	fmt.Printf("plugin OK: %s\n", path)
	return nil
}
