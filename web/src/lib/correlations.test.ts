import { describe, expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { linksOf } from './correlations';
import { runKeyString } from './keys';

describe('runs an announcement naming no pod could be from, each ambiguously linked to the task', () => {
	const s = goldenStory('failed-run-retry');
	const task = { kind: 'task' as const, id: 'vikunja.example.com/tasks/104' };
	const [first, retry] = s.runs.map((r) => ({ kind: 'run' as const, id: runKeyString(r.key) }));
	if (!first || !retry) throw new Error('two runs expected');
	// The collector gives each candidate the announcement's evidence.
	const announced = [
		{
			source: 'vikunja' as const,
			instance: 'vikunja.example.com',
			ref: 'GET /api/v1/tasks/104/comments',
			note: 'comment 5403 names Kelos Task vikunja-widgets-agent-opus-104 but no pod; a retry reuses the name'
		}
	];
	s.correlations = s.correlations.map((c) =>
		c.to.kind === 'task'
			? { ...c, confidence: 'heuristic', ambiguous: true, evidence: announced }
			: c
	);

	test.each([first, retry])('from %o, the candidates are both runs', (run) => {
		const toTask = linksOf(s, run).find((l) => l.other.kind === 'task');
		expect(toTask?.candidateSets).toEqual([[first, retry]]);
	});

	test('from the task, each link names both runs', () => {
		const links = linksOf(s, task);
		expect(links).toHaveLength(2);
		for (const l of links) {
			expect(l.candidateSets).toEqual([[first, retry]]);
		}
	});

	test('a candidate whose alternatives were dropped has no set', () => {
		const lone = structuredClone(s);
		lone.correlations = lone.correlations.filter(
			(c) => !(c.from.id === retry.id && c.to.kind === 'task')
		);
		expect(linksOf(lone, first).find((l) => l.other.kind === 'task')?.candidateSets).toEqual([]);
	});
});
