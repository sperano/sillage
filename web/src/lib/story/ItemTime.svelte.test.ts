import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import ItemTime from './ItemTime.svelte';
import { goldenStory } from '../testing/stories';
import { runRef } from '../timeline';
import type { Interval } from '../model';

function showRun(interval: Interval) {
	const run = goldenStory('announced-run-only').runs[0];
	if (!run) throw new Error('no run');
	run.interval = interval;
	render(ItemTime, { item: { kind: 'run', ref: runRef(run), run }, timeZone: 'UTC' });
}

test('a run with an unknown start and an open end is not ended', async () => {
	showRun({ start: { state: 'unknown' }, end: { state: 'open' } });
	await expect
		.element(page.getByText('start unknown · not ended at the last read'))
		.toBeInTheDocument();
});

test('a run with neither end known says so', async () => {
	showRun({ start: { state: 'unknown' }, end: { state: 'unknown' } });
	await expect.element(page.getByText('start and end unknown')).toBeInTheDocument();
});
