// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Plugin } from 'vite';

/** Vite plugin recording the npm packages present in the shipped bundle. */
export function bundledPackages(): Plugin;
