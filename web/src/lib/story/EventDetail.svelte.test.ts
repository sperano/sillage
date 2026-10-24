import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import EventDetail from './EventDetail.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';
import { timelineItems } from '../timeline';
import type { Event } from '../model';

function show(change: (e: Event) => void) {
	const story = goldenStory('implementation-merged');
	const event = story.events.find((e) => e.kind === 'task.bucket_observed');
	if (!event) throw new Error('no observed event');
	change(event);
	render(EventDetail, {
		story,
		items: timelineItems(story),
		selection: new Selection(),
		event,
		links: {},
		timeZone: 'UTC'
	});
}

test('a state seen again says when it was last seen, not when it changed', async () => {
	show((e) => (e.lastObservedAt = '2026-03-03T18:20:00Z'));
	await expect
		.element(page.getByTestId('last-observed'))
		.toHaveTextContent(
			/18:20.*seen unchanged by every read until then; when it changed is unknown/
		);
});

test('a state seen once has no last observed time', async () => {
	show(() => {});
	await expect.element(page.getByTestId('source-time')).toBeInTheDocument();
	await expect.element(page.getByTestId('last-observed')).not.toBeInTheDocument();
});
