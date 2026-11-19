import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vitest/config';

// The store is plain logic in a `.svelte.ts` file, so the Svelte plugin is what
// compiles its runes. It runs under Node: the few browser globals it touches
// are stubbed in the test.
export default defineConfig({
  plugins: [svelte()],
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
});
