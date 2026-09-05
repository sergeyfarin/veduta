import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/svelte';
import { afterEach } from 'vitest';

// Unmounts every component after each test so one test's DOM never bleeds into the next -
// the frontend equivalent of the "one archive per test" isolation this project's sibling
// project's Playwright config already documents the hard way (see docs/dev-environment.md).
afterEach(() => cleanup());
