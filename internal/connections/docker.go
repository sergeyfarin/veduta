// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// DockerRegistry is the core-only read surface for Docker connections. Plugin brokers receive
// only Registry, whose Do method rejects Docker connections, so this authority is never exposed
// through the extension ABI.
type DockerRegistry interface {
	DockerGET(ctx context.Context, id, path string) (*Response, error)
}

type dockerClient struct {
	client *http.Client
	base   *url.URL
}

var (
	// ErrNotDocker identifies a non-Docker connection passed to DockerGET.
	ErrNotDocker = errors.New("connections: not a docker connection")
	// ErrDockerPath identifies an endpoint outside the fixed read-only Engine API surface.
	ErrDockerPath = errors.New("connections: docker endpoint is not in the read-only allowlist")
)

func newDockerClient(id string, cfg *DockerConfig) (*dockerClient, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("connection %q: invalid Docker endpoint", id)
	}
	transport := &http.Transport{}
	var base *url.URL
	switch endpoint.Scheme {
	case "unix":
		if endpoint.Path == "" || endpoint.Host != "" {
			return nil, fmt.Errorf("connection %q: unix Docker endpoint must contain an absolute socket path", id)
		}
		socket := endpoint.Path
		dialer := &net.Dialer{}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
		base, _ = url.Parse("http://docker")
	case "tcp":
		if endpoint.Host == "" || endpoint.Path != "" {
			return nil, fmt.Errorf("connection %q: tcp Docker endpoint must be tcp://host:port", id)
		}
		base = &url.URL{Scheme: "http", Host: endpoint.Host}
	default:
		return nil, fmt.Errorf("connection %q: Docker endpoint scheme must be unix or tcp", id)
	}
	return &dockerClient{client: &http.Client{Transport: transport, Timeout: cfg.Timeout}, base: base}, nil
}

func (c *dockerClient) get(ctx context.Context, path string) (*Response, error) {
	if !allowedDockerPath(path) {
		return nil, ErrDockerPath
	}
	target, err := url.Parse(path)
	if err != nil {
		return nil, ErrDockerPath
	}
	u := *c.base
	u.Path, u.RawQuery = target.Path, target.RawQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readLimited(resp.Body, defaultMaxResponseBytes)
	if err != nil {
		return nil, err
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

func allowedDockerPath(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || strings.Contains(u.Path, "..") {
		return false
	}
	switch u.Path {
	case "/containers/json", "/_ping", "/version", "/info":
		return true
	default:
		return false
	}
}
