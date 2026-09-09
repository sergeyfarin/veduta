// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notify implements credential-owning notification transports and a durable outbox.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/secrets"
)

// Message is the credential-free payload stored in the outbox and sent to transports.
type Message struct {
	RuleID   string `json:"ruleId"`
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

// Notifier delivers one message.
type Notifier interface {
	Send(context.Context, Message) error
}

// Channel combines a transport with its abuse controls.
type Channel struct {
	Notifier Notifier
	PerHour  int
	Cooldown time.Duration
}

// PermanentError says retrying cannot make the request valid.
type PermanentError struct{ Status int }

func (e PermanentError) Error() string {
	return fmt.Sprintf("notification rejected with HTTP %d", e.Status)
}

// BuildChannels resolves channel secrets once and constructs the configured transports.
func BuildChannels(values map[string]config.Channel, resolved map[string]secrets.Value, client *http.Client) (map[string]Channel, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &safeClient
	out := make(map[string]Channel, len(values))
	for id, value := range values {
		switch value.Type {
		case "webhook":
			if value.Webhook == nil {
				return nil, fmt.Errorf("notify: channel %q has no webhook configuration", id)
			}
			webhook := value.Webhook
			endpoint, err := resolveRef(webhook.URL, resolved)
			if err != nil {
				return nil, fmt.Errorf("notify: resolve channel %q URL: %w", id, err)
			}
			headers := make(map[string]string, len(webhook.Headers))
			for name, ref := range webhook.Headers {
				headers[name], err = resolveRef(ref, resolved)
				if err != nil {
					return nil, fmt.Errorf("notify: resolve channel %q header %q: %w", id, name, err)
				}
			}
			method := webhook.Method
			if method == "" {
				method = http.MethodPost
			}
			out[id] = Channel{Notifier: &webhookNotifier{client: client, endpoint: endpoint, method: method, headers: headers}, PerHour: ratePerHour(webhook.RateLimit.PerHour), Cooldown: rateCooldown(webhook.RateLimit.Cooldown)}
		case "ntfy":
			if value.Ntfy == nil {
				return nil, fmt.Errorf("notify: channel %q has no ntfy configuration", id)
			}
			ntfy := value.Ntfy
			token, err := resolveRef(ntfy.Token, resolved)
			if err != nil {
				return nil, fmt.Errorf("notify: resolve channel %q token: %w", id, err)
			}
			out[id] = Channel{Notifier: &ntfyNotifier{client: client, endpoint: strings.TrimRight(ntfy.URL, "/") + "/" + url.PathEscape(ntfy.Topic), token: token, priority: ntfy.Priority}, PerHour: ratePerHour(ntfy.RateLimit.PerHour), Cooldown: rateCooldown(ntfy.RateLimit.Cooldown)}
		default:
			return nil, fmt.Errorf("notify: unsupported channel %q type %q", id, value.Type)
		}
	}
	return out, nil
}

func ratePerHour(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

func rateCooldown(value string) time.Duration {
	if value == "" {
		return 5 * time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 5 * time.Minute
	}
	return duration
}

func resolveRef(ref config.SecretRef, resolved map[string]secrets.Value) (string, error) {
	if ref.Name != "" {
		value, ok := resolved[ref.Name]
		if !ok {
			return "", errors.New("secret is unresolved")
		}
		return value.Reveal(), nil
	}
	if ref.Template != "" {
		value := ref.Template
		for _, name := range ref.Names {
			secret, ok := resolved[name]
			if !ok {
				return "", errors.New("secret is unresolved")
			}
			value = strings.ReplaceAll(value, "${secret:"+name+"}", secret.Reveal())
		}
		return value, nil
	}
	return ref.Literal, nil
}

type webhookNotifier struct {
	client   *http.Client
	endpoint string
	method   string
	headers  map[string]string
}

// Send delivers one JSON webhook request.
func (n *webhookNotifier) Send(ctx context.Context, message Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, n.method, n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("notify: build webhook request")
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range n.headers {
		request.Header.Set(name, value)
	}
	return send(n.client, request)
}

type ntfyNotifier struct {
	client   *http.Client
	endpoint string
	token    string
	priority int
}

// Send delivers one ntfy publish request.
func (n *ntfyNotifier) Send(ctx context.Context, message Message) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, strings.NewReader(message.Body))
	if err != nil {
		return errors.New("notify: build ntfy request")
	}
	request.Header.Set("Title", message.Title)
	request.Header.Set("Tags", message.Severity)
	if n.priority > 0 {
		request.Header.Set("Priority", strconv.Itoa(n.priority))
	}
	if n.token != "" {
		request.Header.Set("Authorization", "Bearer "+n.token)
	}
	return send(n.client, request)
}

func send(client *http.Client, request *http.Request) error {
	response, err := client.Do(request)
	if err != nil {
		return errors.New("notify: request failed")
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests {
		return PermanentError{Status: response.StatusCode}
	}
	return fmt.Errorf("notify: transient HTTP %d", response.StatusCode)
}
