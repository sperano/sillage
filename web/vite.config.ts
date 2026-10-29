import { defineConfig } from 'vitest/config';
import { playwright } from '@vitest/browser-playwright';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import type { BrowserCommand } from 'vitest/node';

const TEST_TIME_ZONE = 'America/Toronto';

const emulateColorScheme: BrowserCommand<[colorScheme: 'light' | 'dark']> = async (
	context,
	colorScheme
) => {
	if (context.provider.name !== 'playwright') {
		throw new Error(`emulateColorScheme requires Playwright, not ${context.provider.name}`);
	}
	await context.page.emulateMedia({ colorScheme });
};

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
						// A zone with daylight saving time tells local time from UTC.
						provider: playwright({ contextOptions: { timezoneId: TEST_TIME_ZONE } }),
						instances: [{ browser: 'chromium', headless: true }],
						commands: { emulateColorScheme }
					},
					include: ['src/**/*.svelte.test.ts']
				}
			},
			{
				extends: './vite.config.ts',
				test: {
					name: 'node',
					environment: 'node',
					// The browser tests' zone, for the same reason.
					env: { TZ: TEST_TIME_ZONE },
					include: ['src/**/*.test.ts'],
					exclude: ['src/**/*.svelte.test.ts']
				}
			}
		]
	}
});
