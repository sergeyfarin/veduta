// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML decodes the discriminator field first, then re-decodes the same node into the
// concrete type it names. JSON Schema validation (which runs before this, in the loading
// pipeline) already rejects any Kind outside http/docker - the default branch here is defence in
// depth for a caller that decodes without validating first, not the primary gate.
func (c *Connection) UnmarshalYAML(value *yaml.Node) error {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := value.Decode(&probe); err != nil {
		return err
	}
	c.Kind = probe.Kind
	switch probe.Kind {
	case "http":
		var h HTTPConnection
		if err := value.Decode(&h); err != nil {
			return err
		}
		c.HTTP = &h
	case "docker":
		var d DockerConnection
		if err := value.Decode(&d); err != nil {
			return err
		}
		c.Docker = &d
	default:
		return fmt.Errorf("line %d: column %d: unknown connection kind %q", value.Line, value.Column, probe.Kind)
	}
	return nil
}

// UnmarshalYAML mirrors Connection's: probe Type, then decode into the matching concrete type.
func (ch *Channel) UnmarshalYAML(value *yaml.Node) error {
	var probe struct {
		Type string `yaml:"type"`
	}
	if err := value.Decode(&probe); err != nil {
		return err
	}
	ch.Type = probe.Type
	switch probe.Type {
	case "ntfy":
		var n NtfyChannel
		if err := value.Decode(&n); err != nil {
			return err
		}
		ch.Ntfy = &n
	case "webhook":
		var w WebhookChannel
		if err := value.Decode(&w); err != nil {
			return err
		}
		ch.Webhook = &w
	default:
		return fmt.Errorf("line %d: column %d: unknown notification channel type %q", value.Line, value.Column, probe.Type)
	}
	return nil
}
