// SPDX-License-Identifier: AGPL-3.0-or-later

package notify_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/notify"
	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/storage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestBuildChannelsDeliversWebhookAndNtfyWithoutPersistingSecrets(t *testing.T) {
	const secret = "never-store-this-token"
	var mu sync.Mutex
	requests := make([]*http.Request, 0, 2)
	bodies := make([]string, 0, 2)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		mu.Lock()
		requests, bodies = append(requests, request), append(bodies, string(body))
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}
	channels, err := notify.BuildChannels(map[string]config.Channel{
		"hook":  {Type: "webhook", Webhook: &config.WebhookChannel{URL: config.SecretRef{Template: "https://hooks.test/${secret:TOKEN}", Names: []string{"TOKEN"}}, Headers: map[string]config.SecretRef{"Authorization": {Name: "TOKEN"}}}},
		"phone": {Type: "ntfy", Ntfy: &config.NtfyChannel{URL: "https://ntfy.test", Topic: "home", Token: config.SecretRef{Name: "TOKEN"}, Priority: 4}},
	}, map[string]secrets.Value{"TOKEN": secrets.New(secret)}, client)
	if err != nil {
		t.Fatal(err)
	}
	message := notify.Message{RuleID: "disk", Event: "fired", Severity: "critical", Title: "Disk full", Body: "Disk usage is high"}
	for _, id := range []string{"hook", "phone"} {
		if err = channels[id].Notifier.Send(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 || strings.Contains(strings.Join(bodies, ""), secret) {
		t.Fatalf("requests=%d bodies=%q", len(requests), bodies)
	}
	if requests[0].Header.Get("Authorization") != secret || requests[1].Header.Get("Authorization") != "Bearer "+secret {
		t.Fatal("transport did not receive resolved credentials")
	}
}

type failingNotifier struct {
	calls int
	err   error
}

func (n *failingNotifier) Send(context.Context, notify.Message) error { n.calls++; return n.err }

func TestDispatcherRetriesThenAbandons(t *testing.T) {
	store := notificationStore(t)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("down")), Header: http.Header{}}, nil
	})}
	channels, err := notify.BuildChannels(map[string]config.Channel{"hook": {Type: "webhook", Webhook: &config.WebhookChannel{URL: config.SecretRef{Literal: "https://hooks.test/veduta"}}}}, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := notify.NewDispatcher(store, channels, nil, notify.DispatcherConfig{MaxAttempts: 3, RetryBase: time.Nanosecond})
	message := notify.Message{RuleID: "disk", Event: "fired", Severity: "critical", Body: "Disk high"}
	if err = dispatcher.Enqueue(context.Background(), message, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		time.Sleep(time.Microsecond)
		claimed, runErr := dispatcher.RunOnce(context.Background())
		if runErr != nil || !claimed {
			t.Fatalf("claimed=%v err=%v", claimed, runErr)
		}
	}
	var state string
	var attempts int
	if err = store.DB().QueryRow(`SELECT state,attempts FROM notifications`).Scan(&state, &attempts); err != nil || state != "abandoned" || attempts != 3 || calls != 3 {
		t.Fatalf("state=%q attempts=%d calls=%d err=%v", state, attempts, calls, err)
	}
}

func TestDispatcherDedupeAndHourlySuspension(t *testing.T) {
	store := notificationStore(t)
	dispatcher := notify.NewDispatcher(store, map[string]notify.Channel{"hook": {Notifier: &failingNotifier{}, PerHour: 1, Cooldown: time.Hour}}, nil, notify.DispatcherConfig{})
	first := notify.Message{RuleID: "disk", Event: "fired", Body: "first"}
	if err := dispatcher.Enqueue(context.Background(), first, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := dispatcher.RunOnce(context.Background()); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if err := dispatcher.Enqueue(context.Background(), first, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Enqueue(context.Background(), notify.Message{RuleID: "cpu", Event: "fired", Body: "second"}, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Enqueue(context.Background(), notify.Message{RuleID: "memory", Event: "fired", Body: "third"}, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	var notifications, suspensions int
	if err := store.DB().QueryRow(`SELECT count(*) FROM notifications`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM events WHERE type='notification.suspended'`).Scan(&suspensions); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 || suspensions != 1 {
		t.Fatalf("notifications=%d suspensions=%d", notifications, suspensions)
	}
}

func TestOutboxPayloadNeverContainsChannelSecret(t *testing.T) {
	store := notificationStore(t)
	dispatcher := notify.NewDispatcher(store, map[string]notify.Channel{"hook": {Notifier: &failingNotifier{}}}, nil, notify.DispatcherConfig{})
	if err := dispatcher.Enqueue(context.Background(), notify.Message{RuleID: "disk", Event: "fired", Body: "Disk high"}, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := store.DB().QueryRow(`SELECT payload FROM notifications`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "secret") || strings.Contains(payload, "token") {
		t.Fatalf("credential-like data in outbox payload: %s", payload)
	}
}

func notificationStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
