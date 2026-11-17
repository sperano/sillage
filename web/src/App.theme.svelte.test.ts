import { afterEach, describe, expect, test } from 'vitest';
import { commands, page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import './app.css';
import App from './App.svelte';
import { API_BASE } from './lib/api';
import { contrast, GRAPHIC_CONTRAST, parseColor, TEXT_CONTRAST } from './lib/contrast';
import { memoryHistory } from './lib/history';
import { Router } from './lib/router.svelte';
import { fakeServer, json } from './testing/fakeServer';

const THEMES = {
	light: {
		background: 'rgb(243, 245, 247)',
		foreground: 'rgb(23, 42, 58)',
		muted: 'rgb(83, 99, 115)'
	},
	dark: {
		background: 'rgb(15, 22, 29)',
		foreground: 'rgb(227, 233, 239)',
		muted: 'rgb(159, 173, 186)'
	}
};
const SCHEMES = ['light', 'dark'] as const;

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

/** A colour as the page computes it: a token, or a color-mix() of tokens. */
function computed(value: string): string {
	const probe = document.createElement('div');
	probe.style.setProperty('background-color', value);
	document.body.append(probe);
	try {
		return getComputedStyle(probe).backgroundColor;
	} finally {
		probe.remove();
	}
}

/** The contrast between two colours, each a token or a mix of tokens. */
function ratio(foreground: string, background: string): number {
	return contrast(parseColor(computed(foreground)), parseColor(computed(background)));
}

const token = (name: string) => `var(--color-${name})`;
const tint = (name: string, percent: number) =>
	`color-mix(in srgb, ${token(name)} ${percent}%, ${token('surface')})`;

/** Text and what it is read on, as the components draw them. */
const TEXT_PAIRS: [text: string, background: string][] = [
	...['fg', 'muted', 'link'].flatMap((t) => [
		[token(t), token('bg')] as [string, string],
		[token(t), token('surface')] as [string, string]
	]),
	[token('header-fg'), token('header-bg')],
	// The overview's running bar.
	[token('on-running'), token('running')],
	// The overview's chips (TaskRow.svelte) and the selected event (EventList.svelte).
	[token('fg'), tint('muted', 15)],
	[token('fg'), tint('running', 25)],
	[token('fg'), tint('review', 25)],
	[token('fg'), tint('failure', 20)],
	[token('fg'), tint('pr', 25)],
	[token('fg'), tint('selected', 20)],
	// A span bar's text inside its fill (SpanBar.svelte, fill-opacity 0.35).
	...['running', 'waiting', 'review', 'pr'].map(
		(bar) => [token('fg'), tint(bar, 35)] as [string, string]
	)
];

/** Bars, marks, rings and control borders, on the surface or page they sit on. */
const GRAPHIC_PAIRS: [mark: string, background: string][] = [
	...[
		'event',
		'running',
		'waiting',
		'review',
		'pr',
		'success',
		'failure',
		'merged',
		'unknown',
		'selected',
		'cursor'
	].map((mark) => [token(mark), token('surface')] as [string, string]),
	...['focus', 'control-border'].flatMap((mark) => [
		[token(mark), token('bg')] as [string, string],
		[token(mark), token('surface')] as [string, string]
	])
];

describe.each(SCHEMES)('the %s scheme', (colorScheme) => {
	test.each(TEXT_PAIRS)('text %s on %s meets WCAG AA', async (text, background) => {
		await commands.emulateColorScheme(colorScheme);
		expect(ratio(text, background)).toBeGreaterThanOrEqual(TEXT_CONTRAST);
	});

	test.each(GRAPHIC_PAIRS)('mark %s on %s stands out', async (mark, background) => {
		await commands.emulateColorScheme(colorScheme);
		expect(ratio(mark, background)).toBeGreaterThanOrEqual(GRAPHIC_CONTRAST);
	});
});
