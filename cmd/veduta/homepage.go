// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/homepageimport"
)

func importCmd(args []string) error {
	if len(args) == 0 || args[0] != "homepage" {
		return errors.New("usage: veduta import homepage --dir <homepage-config> [--config veduta.yaml]")
	}
	return importHomepage(args[1:], os.Stdout, os.Stderr)
}

func importHomepage(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("import homepage", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dir := fs.String("dir", "", "directory containing Homepage services.yaml and related files")
	configPath := fs.String("config", "veduta.yaml", "Veduta configuration to update")
	pluginDir := fs.String("plugin-dir", "plugins", "plugin directory written relative to the Veduta config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("import homepage: --dir is required")
	}
	model, parseWarnings, err := homepageimport.ParseDir(*dir)
	if err != nil {
		return fmt.Errorf("parse Homepage config: %w", err)
	}
	existing, diagnostics := config.LoadPath(*configPath)
	if existing == nil || diagnostics.HasErrors() {
		return fmt.Errorf("load Veduta config:\n%s", diagnostics.String())
	}
	patch, report, mapWarnings := homepageimport.Map(model, existing, homepageimport.MapOptions{PluginDir: *pluginDir})
	for _, warning := range append(parseWarnings, mapWarnings...) {
		_, _ = fmt.Fprintf(errOut, "warning: %s:%s: %s\n", warning.Source, warning.Path, warning.Message)
	}
	if err = config.Apply(*configPath, patch); err != nil {
		return fmt.Errorf("apply Homepage import: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Imported %d services · %d complete · %d without widgets · %d need manual configuration\n", report.Services, report.Complete, report.WithoutWidgets, report.NeedManual)
	if len(report.EnvironmentVariables) > 0 {
		_, _ = fmt.Fprintln(out, "Set these environment variables before enabling imported connections:")
		for _, name := range report.EnvironmentVariables {
			_, _ = fmt.Fprintln(out, "  "+name)
		}
	}
	return nil
}
