// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect, beforeEach } from 'vitest';
import {
  apply,
  applyAppearance,
  next,
  nextAppearance,
  stored,
  storedAppearance
} from './theme';

const root = () => document.documentElement;

beforeEach(() => {
  localStorage.clear();
  delete root().dataset.theme;
  delete root().dataset.appearance;
  delete root().dataset.backdrop;
});

describe('the colour scheme axis', () => {
  it('writes an explicit choice as an attribute and removes it for auto', () => {
    apply('dark');
    expect(root().dataset.theme).toBe('dark');
    apply('auto');
    // Not data-theme="auto": the absence of the attribute is what lets the CSS fall through to
    // prefers-color-scheme, which is the only difference between following the system and not.
    expect(root().dataset.theme).toBeUndefined();
  });

  it('remembers the choice, auto included', () => {
    apply('light');
    expect(stored()).toBe('light');
    apply('auto');
    expect(stored()).toBe('auto');
  });

  it('falls back to auto for a value it did not write', () => {
    localStorage.setItem('veduta.theme', 'sepia');
    expect(stored()).toBe('auto');
  });
});

describe('the preset axis', () => {
  it('defers to the configured preset while the preference is auto', () => {
    applyAppearance('auto', 'veil');
    expect(root().dataset.appearance).toBe('veil');
    applyAppearance('auto', 'clean');
    expect(root().dataset.appearance).toBeUndefined();
  });

  it('lets a viewer turn Veil on where the instance is configured clean', () => {
    applyAppearance('veil', 'clean');
    expect(root().dataset.appearance).toBe('veil');
  });

  it('lets a viewer turn Veil off where the instance is configured veil', () => {
    applyAppearance('clean', 'veil');
    expect(root().dataset.appearance).toBeUndefined();
  });

  it('treats an unknown configured preset as clean rather than as an error', () => {
    // The enum widens as presets ship, so an older frontend can be handed a newer preset name.
    // Rendering the default is the only safe reading of one it does not have CSS for.
    applyAppearance('auto', 'lagoon');
    expect(root().dataset.appearance).toBeUndefined();
  });

  it('survives the layout not having arrived yet', () => {
    applyAppearance('auto', undefined);
    expect(root().dataset.appearance).toBeUndefined();
  });

  it('remembers the preference and not the resolved value', () => {
    // Storing 'veil' here would freeze an instance default into the viewer's browser, so that
    // changing dashboard.appearance later would stop reaching anyone who had ever loaded the page.
    applyAppearance('auto', 'veil');
    expect(storedAppearance()).toBe('auto');
    applyAppearance('clean', 'veil');
    expect(storedAppearance()).toBe('clean');
  });

  it('falls back to auto for a value it did not write', () => {
    localStorage.setItem('veduta.appearance', 'veil-but-bluer');
    expect(storedAppearance()).toBe('auto');
  });
});

describe('the configured background', () => {
  it('is used only when the effective preset is veil', () => {
    applyAppearance('auto', 'veil', true);
    expect(root().dataset.backdrop).toBe('image');
    // Turning the preset off takes the backdrop with it - Clean has no backdrop layer at all.
    applyAppearance('clean', 'veil', true);
    expect(root().dataset.backdrop).toBeUndefined();
  });

  it('leaves the bundled painting in place when the instance configured none', () => {
    applyAppearance('veil', 'clean', false);
    expect(root().dataset.appearance).toBe('veil');
    expect(root().dataset.backdrop).toBeUndefined();
  });
});

describe('cycling', () => {
  it('returns to auto, so every control can get back to its default', () => {
    expect(next(next(next('auto')))).toBe('auto');
    expect(nextAppearance(nextAppearance(nextAppearance('auto')))).toBe('auto');
  });

  it('reaches every value', () => {
    expect([next('auto'), next('light'), next('dark')]).toEqual(['light', 'dark', 'auto']);
    expect([nextAppearance('auto'), nextAppearance('clean'), nextAppearance('veil')])
      .toEqual(['clean', 'veil', 'auto']);
  });
});
