import { describe, expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { linksOf } from './correlations';
import { eventKey, runKeyString } from './keys';

const story = goldenStory('wiki-rooted-request');

test('an ambiguous correlation lists every candidate', () => {
	const live = story.events.find((e) => e.details?.version === 'live');
	if (!live) throw new Error('no live edit');
	const links = linksOf(story, { kind: 'event', id: eventKey(live) });
	expect(links).toHaveLength(2);
	for (const l of links) {
		expect(l.correlation.confidence).toBe('heuristic');
		expect(l.candidateSets.map((set) => set.map((c) => c.id))).toEqual([
			[
				'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057',
				'cluster-a/agents/uid/00000000-0000-4000-8000-000000000103'
			]
		]);
	}
});

test('from a candidate run, the other candidate is listed too', () => {
	const links = linksOf(story, {
		kind: 'run',
		id: 'cluster-a/agents/uid/00000000-0000-4000-8000-000000000103'
	});
	expect(links).toHaveLength(1);
	expect(links[0]?.candidateSets.map((set) => set.length)).toEqual([2]);
});

test('a correlation is seen from both ends; an unambiguous one has no candidates', () => {
	const run = 'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057';
	const links = linksOf(story, { kind: 'run', id: run });
	expect(links.map((l) => l.other.kind).sort()).toEqual([
		'event',
		'event',
		'event',
		'event',
		'page'
	]);
	const announced = links.find((l) => l.other.id === 'wiki/wiki.example.com/comments/881');
	expect(announced?.correlation.confidence).toBe('identifier');
	expect(announced?.candidateSets).toEqual([]);
});

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

test('two events each ambiguous between the same runs are not alternatives to each other', () => {
	const s = goldenStory('wiki-rooted-request');
	const live = s.events.find((e) => e.details?.version === 'live');
	if (!live) throw new Error('no live edit');
	const liveKey = eventKey(live);
	const other = { ...live, id: 'sha256:' + 'f'.repeat(64), sourceTime: '2026-03-04T15:17:00Z' };
	s.events.push(other);
	for (const c of s.correlations.filter((x) => x.from.id === liveKey)) {
		s.correlations.push({
			...c,
			from: { kind: 'event', id: eventKey(other) },
			evidence: [{ ...c.evidence[0]!, note: `${other.id} by agent-opus inside the run's interval` }]
		});
	}
	const runs = [
		'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057',
		'cluster-a/agents/uid/00000000-0000-4000-8000-000000000103'
	];
	for (const l of linksOf(s, { kind: 'event', id: liveKey })) {
		expect(l.candidateSets.map((set) => set.map((r) => r.id))).toEqual([runs]);
	}
	const fromRun = linksOf(s, { kind: 'run', id: runs[0]! }).filter((l) => l.correlation.ambiguous);
	expect(fromRun).toHaveLength(2);
	for (const l of fromRun) {
		expect(l.candidateSets.map((set) => set.map((r) => r.id))).toEqual([runs]);
	}
});

test('events whose other candidates were dropped are not grouped by the run they share', () => {
	const s = goldenStory('wiki-rooted-request');
	const run = 'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057';
	s.correlations = s.correlations.map((c) =>
		c.to.id === run && c.confidence === 'heuristic' ? { ...c, ambiguous: true } : c
	);
	s.correlations = s.correlations.filter((c) => !(c.ambiguous && c.to.id !== run));
	for (const l of linksOf(s, { kind: 'run', id: run }).filter((x) => x.correlation.ambiguous)) {
		expect(l.candidateSets).toEqual([]);
	}
});
