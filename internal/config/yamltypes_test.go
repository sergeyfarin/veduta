// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"veduta.dev/veduta/internal/config"
)

func TestConnection_HTTP(t *testing.T) {
	var c config.Connection
	err := yaml.Unmarshal([]byte(`
kind: http
baseUrl: http://immich:2283
auth: { type: header, name: x-api-key, value: "${secret:IMMICH_KEY}" }
`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != "http" || c.HTTP == nil || c.Docker != nil {
		t.Fatalf("got %+v", c)
	}
	if c.HTTP.BaseURL != "http://immich:2283" {
		t.Fatalf("BaseURL = %q", c.HTTP.BaseURL)
	}
	if c.HTTP.Auth.Value.Name != "IMMICH_KEY" {
		t.Fatalf("Auth.Value = %+v", c.HTTP.Auth.Value)
	}
}

func TestConnection_Docker(t *testing.T) {
	var c config.Connection
	err := yaml.Unmarshal([]byte("kind: docker\nendpoint: tcp://docker-socket-proxy:2375\n"), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != "docker" || c.Docker == nil || c.HTTP != nil {
		t.Fatalf("got %+v", c)
	}
	if c.Docker.Endpoint != "tcp://docker-socket-proxy:2375" {
		t.Fatalf("Endpoint = %q", c.Docker.Endpoint)
	}
}

func TestConnection_UnknownKindIsError(t *testing.T) {
	var c config.Connection
	err := yaml.Unmarshal([]byte("kind: ssh\nhost: x\n"), &c)
	if err == nil {
		t.Fatal("want an error for an unknown connection kind")
	}
}

func TestChannel_Ntfy(t *testing.T) {
	var ch config.Channel
	err := yaml.Unmarshal([]byte("type: ntfy\nurl: https://ntfy.sh\ntopic: veduta-home\n"), &ch)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Type != "ntfy" || ch.Ntfy == nil || ch.Webhook != nil {
		t.Fatalf("got %+v", ch)
	}
	if ch.Ntfy.Topic != "veduta-home" {
		t.Fatalf("Topic = %q", ch.Ntfy.Topic)
	}
}

func TestChannel_Webhook(t *testing.T) {
	var ch config.Channel
	err := yaml.Unmarshal([]byte("type: webhook\nurl: http://n8n.lan/webhook/veduta\n"), &ch)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Type != "webhook" || ch.Webhook == nil || ch.Ntfy != nil {
		t.Fatalf("got %+v", ch)
	}
}

func TestChannel_UnknownTypeIsError(t *testing.T) {
	var ch config.Channel
	err := yaml.Unmarshal([]byte("type: sms\n"), &ch)
	if err == nil {
		t.Fatal("want an error for an unknown channel type")
	}
}
