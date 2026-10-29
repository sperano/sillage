import { afterEach, expect, test } from 'vitest';
import { commands, page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import App from './App.svelte';
import { API_BASE } from './lib/api';
import { memoryHistory } from './lib/history';
import { Router } from './lib/router.svelte';
import { fakeServer, json } from './testing/fakeServer';

const THEMES = {
	light: {
		background: 'rgb(255, 255, 255)',
		foreground: 'rgb(29, 39, 51)',
		muted: 'rgb(91, 107, 124)'
	},
	dark: {
		background: 'rgb(16, 22, 29)',
		foreground: 'rgb(227, 233, 239)',
		muted: 'rgb(154, 168, 182)'
	}
};

afterEach(async () => {
	await commands.emulateColorScheme(null);
});

test.each([
	{ colorScheme: 'light' as const, expected: THEMES.light },
	{ colorScheme: 'dark' as const, expected: THEMES.dark }
])('the page follows the $colorScheme system theme', async ({ colorScheme, expected }) => {
	await commands.emulateColorScheme(colorScheme);
	const server = fakeServer({
		[`${API_BASE}/coverage`]: () => json({ sources: [], off: [] }),
		[`${API_BASE}/search`]: () =>
			json({
				query: 'widgets',
				roots: [{ root: { task: { instance: 'vikunja.example.com', id: 102 } }, title: 'Widgets' }]
			})
	});
	const router = new Router(memoryHistory('/?q=widgets'));
	await render(App, { api: server.api, router });
	await expect.element(page.getByRole('link', { name: 'Widgets' })).toBeVisible();

	const key = page.getByText('task 102 on vikunja.example.com', { exact: true });
	const rootStyle = getComputedStyle(document.documentElement);
	const bodyStyle = getComputedStyle(document.body);
	const mutedStyle = getComputedStyle(key.element());
	expect(rootStyle.colorScheme).toBe('light dark');
	expect(bodyStyle.backgroundColor).toBe(expected.background);
	expect(bodyStyle.color).toBe(expected.foreground);
	expect(mutedStyle.color).toBe(expected.muted);
});
