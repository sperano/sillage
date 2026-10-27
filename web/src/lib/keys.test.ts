import { describe, expect, test } from 'vitest';
import { goldenStories } from './testing/stories';
import { eventKey, prKeyString, runKeyString, taskKeyString } from './keys';
import type { Ref, Story } from './model';

function known(story: Story): Set<string> {
	const refs = new Set<string>();
	const add = (kind: Ref['kind'], id: string) => refs.add(`${kind}:${id}`);
	add('task', taskKeyString(story.root.task));
	for (const e of story.events) {
		add('event', eventKey(e));
		if (e.related.task) add('task', taskKeyString(e.related.task));
	}
	for (const r of story.runs) add('run', runKeyString(r.key));
	for (const p of story.pullRequests) add('pr', prKeyString(p.key));
	return refs;
}

test('every golden story is loaded', () => {
	expect([...goldenStories.keys()].sort()).toEqual([
		'announced-run-only',
		'checks-after-force-push',
		'discuss-no-pr',
		'failed-run-retry',
		'implementation-merged',
		'phase-markers',
		'reopened-task',
		'second-pr-after-close'
	]);
});

describe.each([...goldenStories])('%s', (_, story) => {
	test('every correlation end is a key of the story, as the Go model writes it', () => {
		const refs = known(story);
		expect(refs.size).toBeGreaterThan(0);
		for (const c of story.correlations) {
			expect(refs).toContain(`${c.from.kind}:${c.from.id}`);
			expect(refs).toContain(`${c.to.kind}:${c.to.id}`);
		}
	});
});
