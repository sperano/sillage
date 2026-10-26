import { expect, test } from 'vitest';
import { goldenStories, goldenStory } from './testing/stories';
import { timelineItems } from './timeline';

test.each([...goldenStories])(
	'%s: every event, run, check and phase is listed once, in time order',
	(_, story) => {
		const items = timelineItems(story);
		const checks = story.pullRequests.reduce((n, p) => n + p.checks.length, 0);
		const phases = story.runs.reduce((n, r) => n + (r.phases?.length ?? 0), 0);
		expect(items).toHaveLength(story.events.length + story.runs.length + checks + phases);
		expect(new Set(items.map((i) => `${i.ref.kind}:${i.ref.id}`)).size).toBe(items.length);
		const timed = items.filter((i) => i.time !== undefined).map((i) => Date.parse(i.time ?? ''));
		expect(timed).toEqual([...timed].sort((a, b) => a - b));
		const firstUntimed = items.findIndex((i) => i.time === undefined);
		if (firstUntimed >= 0) {
			expect(items.slice(firstUntimed).every((i) => i.time === undefined)).toBe(true);
		}
	}
);

test('a run with no known bound comes last, unplaced', () => {
	const items = timelineItems(goldenStory('announced-run-only'));
	const last = items.at(-1);
	expect(last?.kind).toBe('run');
	expect(last?.time).toBeUndefined();
});

test('a polling observation sits at its observed time', () => {
	const items = timelineItems(goldenStory('implementation-merged'));
	const review = items.find((i) => i.kind === 'event' && i.event.details?.bucket === 'Review');
	expect(review?.time).toBe('2026-03-03T18:26:00Z');
});

test('the retry is a second run row', () => {
	const runs = timelineItems(goldenStory('failed-run-retry')).filter((i) => i.kind === 'run');
	expect(runs.map((r) => r.ref.id)).toEqual([
		'cluster-a/agents/uid/00000000-0000-4000-8000-00000000104a',
		'cluster-a/agents/uid/00000000-0000-4000-8000-00000000104b'
	]);
});

test('a phase sits at its first recorded edge', () => {
	const items = timelineItems(goldenStory('phase-markers'));
	const phases = items.filter((i) => i.kind === 'phase');
	expect(phases).toHaveLength(5);
	for (const item of phases) {
		expect(item.time).toBe(
			item.kind === 'phase' ? (item.phase.start?.at ?? item.phase.end?.at) : undefined
		);
	}
	expect(new Set(phases.map((i) => i.ref.id.split('/phases/')[0])).size).toBe(2);
});
