// SPDX-License-Identifier: AGPL-3.0-or-later

import type { CardState } from './types/cardstate';

export type StreamStatus = 'connecting' | 'live' | 'offline';

export function connectCardStream(
  onCard: (card: CardState) => void,
  onReset: () => void,
  onStatus: (status: StreamStatus) => void
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
  return () => stream.close();
}
