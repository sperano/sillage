import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Bounds from './Bounds.svelte';

test('both ends known but unreadable: no duration, and why', async () => {
	render(Bounds, {
		interval: { start: { state: 'known', at: 'not a time' }, end: { state: 'known', at: 'later' } },
		timeZone: 'UTC'
	});
	await expect
		.element(page.getByTestId('duration'))
		.toHaveTextContent('none: a recorded time could not be read');
});

test('an open end is named as open', async () => {
	render(Bounds, {
		interval: { start: { state: 'unknown' }, end: { state: 'open' } },
		timeZone: 'UTC'
	});
	await expect
		.element(page.getByTestId('duration'))
		.toHaveTextContent('none: the start is unknown and the end is open');
});
