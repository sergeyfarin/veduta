// SPDX-License-Identifier: AGPL-3.0-or-later

// Maps a Widget Document block's "type" discriminator to the Svelte component that renders it.
// BlockRenderer.svelte is the only consumer; nothing else should import this map directly, so
// that adding a block type is a one-line change in exactly one place. Every v1 block type is
// registered here; `series` joined in Phase M3.
import type { Component } from 'svelte';
import MetricsBlock from './MetricsBlock.svelte';
import KeyValueBlock from './KeyValueBlock.svelte';
import ProgressBlock from './ProgressBlock.svelte';
import StatusListBlock from './StatusListBlock.svelte';
import ListBlock from './ListBlock.svelte';
import TextBlock from './TextBlock.svelte';
import MediaBlock from './MediaBlock.svelte';
import TableBlock from './TableBlock.svelte';
import ActionsBlock from './ActionsBlock.svelte';
import SeriesBlock from './SeriesBlock.svelte';

// `Component<any>`: each concrete block component's prop type is a specific narrowed Block
// variant (via Extract<...>), which is intentionally incompatible with a single generic map
// value type - the map's whole job is to erase that difference so BlockRenderer can look one
// up by a runtime string. BlockRenderer is the sole caller and re-establishes type safety by
// construction: it always passes the same `block` object the type came from.
export const registry: Record<string, Component<any>> = {
  metrics: MetricsBlock,
  'key-value': KeyValueBlock,
  progress: ProgressBlock,
  status: StatusListBlock,
  list: ListBlock,
  text: TextBlock,
  markdown: TextBlock,
  image: MediaBlock,
  'image-grid': MediaBlock,
  'poster-grid': MediaBlock,
  table: TableBlock,
  actions: ActionsBlock,
  series: SeriesBlock
};
