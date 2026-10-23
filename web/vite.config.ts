import { defineConfig } from 'vitest/config';
import { playwright } from '@vitest/browser-playwright';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// A static single-page application: the Go binary embeds dist/ and serves
// it next to its API, so there is no Node server at runtime.
export default defineConfig({
	plugins: [svelte()],
	build: {
		outDir: 'dist',
		// dist/.vite/license.md: the licenses of the dependencies in the bundle,
		// which the image ships under /licenses. go:embed skips dot directories,
		// so the binary does not serve it.
		license: true
	},
	test: {
		expect: { requireAssertions: true },
		projects: [
			{
				extends: './vite.config.ts',
				test: {
					name: 'client',
					browser: {
						enabled: true,
						// A zone other than UTC, with daylight saving time, so that the
						// tests tell local time from UTC whatever zone the machine runs in.
						provider: playwright({ contextOptions: { timezoneId: 'America/Toronto' } }),
						instances: [{ browser: 'chromium', headless: true }]
					},
					include: ['src/**/*.svelte.test.ts']
				}
			},
			{
				extends: './vite.config.ts',
				test: {
					name: 'node',
					environment: 'node',
					include: ['src/**/*.test.ts'],
					exclude: ['src/**/*.svelte.test.ts']
				}
			}
		]
	}
});
