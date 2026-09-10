// SPDX-License-Identifier: AGPL-3.0-or-later

// Hand-written, not generated: GET /dashboard's shape is presentation-only layout, owned by
// configuration once Phase C exists. Until then --fixtures is its only producer
// (internal/fixtures.Dashboard), so this mirrors that Go type rather than a schema.
import type { Span } from '../types';

export interface CardDescriptor {
  id: string;
  title: string;
  icon?: string;
  href?: string;
  span?: Span;
}

export interface DashboardSection {
  title?: string;
  cards: CardDescriptor[];
}

/** The visual preset chosen by dashboard.appearance. Not the light/dark axis - see lib/theme.ts. */
export type Appearance = 'clean' | 'veil';

export interface Dashboard {
  appearance?: Appearance;
  /** True when the instance configured a background image, served from /api/v1/background. */
  background?: boolean;
  sections: DashboardSection[];
}
