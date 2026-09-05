// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import UnknownBlock from './UnknownBlock.svelte';

describe('UnknownBlock', () => {
  it('names the unrecognised type rather than rendering blank', () => {
    render(UnknownBlock, { props: { type: 'chart' } });
    expect(screen.getByText('chart')).toBeInTheDocument();
  });
});
