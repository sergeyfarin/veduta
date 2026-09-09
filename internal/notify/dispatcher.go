// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/storage"
)

// DispatcherConfig controls bounded retry behaviour.
type DispatcherConfig struct {
	MaxAttempts int
	RetryBase   time.Duration
	PollEvery   time.Duration
}

// Dispatcher owns channel configuration and drains the persistent outbox.
type Dispatcher struct {
	store    *storage.Store
	logger   *slog.Logger
	config   DispatcherConfig
	mu       sync.RWMutex
	channels map[string]Channel
	wake     chan struct{}
}

// NewDispatcher creates a dispatcher. Call Run in one goroutine to drain continuously.
func NewDispatcher(store *storage.Store, channels map[string]Channel, logger *slog.Logger, cfg DispatcherConfig) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = time.Second
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = time.Second
	}
	return &Dispatcher{store: store, logger: logger, config: cfg, channels: cloneChannels(channels), wake: make(chan struct{}, 1)}
}

// Apply atomically replaces notification transports after configuration reload.
func (d *Dispatcher) Apply(channels map[string]Channel) {
	d.mu.Lock()
	d.channels = cloneChannels(channels)
	d.mu.Unlock()
}

// Channels returns a shallow copy suitable for configuration rollback.
func (d *Dispatcher) Channels() map[string]Channel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return cloneChannels(d.channels)
}

func cloneChannels(values map[string]Channel) map[string]Channel {
	out := make(map[string]Channel, len(values))
	for id, value := range values {
		out[id] = value
	}
	return out
}

// Enqueue stores one message for every named channel.
func (d *Dispatcher) Enqueue(ctx context.Context, message Message, channelIDs []string) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, id := range channelIDs {
		channel, ok := d.channels[id]
		if !ok {
			return fmt.Errorf("notify: channel %q is not configured", id)
		}
		key := id + ":" + message.RuleID + ":" + message.Event
		enqueued, _, enqueueErr := d.store.EnqueueNotification(ctx, id, key, payload, time.Now().UTC(), channel.Cooldown, channel.PerHour)
		if enqueueErr != nil {
			return enqueueErr
		}
		if enqueued {
			select {
			case d.wake <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

// Run drains the outbox until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) error {
	if err := d.store.ResetSendingNotifications(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(d.config.PollEvery)
	defer ticker.Stop()
	for {
		for {
			delivered, err := d.RunOnce(ctx)
			if err != nil {
				d.logger.Error("notification dispatcher", "error", err)
				break
			}
			if !delivered {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

// RunOnce attempts one due notification and reports whether one was claimed.
func (d *Dispatcher) RunOnce(ctx context.Context) (bool, error) {
	item, ok, err := d.store.ClaimNotification(ctx, time.Now().UTC())
	if err != nil || !ok {
		return ok, err
	}
	var message Message
	if err = json.Unmarshal(item.Payload, &message); err != nil {
		return true, d.store.FailNotification(ctx, item.ID, time.Time{}, "invalid outbox payload", true)
	}
	d.mu.RLock()
	channel, exists := d.channels[item.Channel]
	d.mu.RUnlock()
	if !exists {
		return true, d.store.FailNotification(ctx, item.ID, time.Time{}, "channel is not configured", true)
	}
	err = channel.Notifier.Send(ctx, message)
	if err == nil {
		return true, d.store.CompleteNotification(ctx, item.ID)
	}
	var permanent PermanentError
	abandon := item.Attempts >= d.config.MaxAttempts || errors.As(err, &permanent)
	delay := d.config.RetryBase
	for attempt := 1; attempt < item.Attempts && delay < time.Hour; attempt++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return true, d.store.FailNotification(ctx, item.ID, time.Now().Add(delay), secrets.DefaultRegistry().Scrub(err.Error()), abandon)
}
