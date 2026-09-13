// SPDX-License-Identifier: AGPL-3.0-or-later

import type { CardState } from './types/cardstate';

export type StreamStatus = 'connecting' | 'live' | 'offline';

export function connectCardStream(
  onCard: (card: CardState) => void,
  onReset: () => void,
  onStatus: (status: StreamStatus) => void,
  onConfig: () => void = () => {}
): () => void {
  if (typeof EventSource === 'undefined') {
    onStatus('offline');
    return () => {};
  }
  onStatus('connecting');
  const stream = new EventSource('/api/v1/stream');
  stream.addEventListener('open', () => onStatus('live'));
  stream.addEventListener('error', () => onStatus('offline'));
  stream.addEventListener('card', (event) => {
    try {
      onCard(JSON.parse((event as MessageEvent<string>).data) as CardState);
    } catch {
      onReset();
    }
  });
  stream.addEventListener('reset', onReset);
  // A new configuration generation changes the LAYOUT - cards added or removed, titles, spans,
  // appearance - none of which a card-state event can express. A client disconnected when it was
  // sent either replays it from the server's ring on reconnect, or receives 'reset' instead
  // because the ring moved past, and 'reset' already means refetch everything.
  stream.addEventListener('config', onConfig);
  return () => stream.close();
}
