import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import ItemTime from './ItemTime.svelte';
import { goldenStory } from '../testing/stories';
import { eventRef, phaseRef, runRef } from '../timeline';
import type { Event, Interval, Phase } from '../model';

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

function showObserved(change: (e: Event) => void) {
	const event = goldenStory('implementation-merged').events.find(
		(e) => e.kind === 'task.bucket_observed'
	);
	if (!event) throw new Error('no observed event');
	change(event);
	render(ItemTime, { item: { kind: 'event', ref: eventRef(event), event }, timeZone: 'UTC' });
}

test('a state seen again says when it was first and last seen', async () => {
	showObserved((e) => (e.lastObservedAt = '2026-03-03T18:20:00Z'));
	await expect.element(page.getByText(/· last seen/)).toBeInTheDocument();
	await expect.element(page.getByText(/no source time/)).toBeInTheDocument();
});

test('a state seen once says only when it was seen', async () => {
	showObserved(() => {});
	await expect.element(page.getByText(/last seen/)).not.toBeInTheDocument();
});

function showPhase(match: (p: Phase) => boolean) {
	const run = goldenStory('phase-markers').runs.find((r) => r.phases?.some(match));
	const phase = run?.phases?.find(match);
	if (!run || !phase) throw new Error('no such phase');
	render(ItemTime, {
		item: { kind: 'phase', ref: phaseRef(run, phase), run, phase },
		timeZone: 'UTC'
	});
}

test('a phase whose start was not recorded says when it ended', async () => {
	showPhase((p) => p.state === 'start-not-recorded');
	await expect.element(page.getByText(/^start not recorded · ended /)).toBeInTheDocument();
});
