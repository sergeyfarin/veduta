// SPDX-License-Identifier: AGPL-3.0-or-later

import { afterEach, describe, expect, it, vi } from 'vitest';
import { connectCardStream } from './stream';

class FakeEventSource {
  static current: FakeEventSource;
  listeners = new Map<string, EventListener[]>();
  close = vi.fn();
  constructor(public url: string) { FakeEventSource.current = this; }
  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }
  emit(type: string, data = '') {
    const event = type === 'card' ? new MessageEvent(type, { data }) : new Event(type);
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}

describe('connectCardStream', () => {
  afterEach(() => vi.unstubAllGlobals());
  it('applies complete card envelopes, requests a refetch on reset, and closes', () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    const cards = vi.fn();
    const reset = vi.fn();
    const status = vi.fn();
    const disconnect = connectCardStream(cards, reset, status);
    expect(FakeEventSource.current.url).toBe('/api/v1/stream');
    FakeEventSource.current.emit('open');
    FakeEventSource.current.emit('card', JSON.stringify({ cardId: 'immich', document: null, execution: { state: 'pending' } }));
    FakeEventSource.current.emit('reset');
    expect(cards).toHaveBeenCalledWith(expect.objectContaining({ cardId: 'immich' }));
    expect(reset).toHaveBeenCalledOnce();
    expect(status).toHaveBeenLastCalledWith('live');
    disconnect();
    expect(FakeEventSource.current.close).toHaveBeenCalledOnce();
  });

  // A configuration generation changes the layout, which card states cannot express - before the
  // stream carried this, an added or removed card needed a browser reload to appear.
  it('reports a new configuration generation so the layout can be refetched', () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    const config = vi.fn();
    connectCardStream(vi.fn(), vi.fn(), vi.fn(), config);
    FakeEventSource.current.emit('config');
    expect(config).toHaveBeenCalledOnce();
  });

  // Omitted by callers that do not care, and by the existing test above: a missing handler must
  // not throw when the server sends one anyway.
  it('tolerates a configuration event with no handler supplied', () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    connectCardStream(vi.fn(), vi.fn(), vi.fn());
    expect(() => FakeEventSource.current.emit('config')).not.toThrow();
  });
});
