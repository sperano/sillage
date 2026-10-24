import { describe, expect, test } from 'vitest';
import type { Phase } from './model';
import { endText, phaseFirstAt, phaseText } from './phases';
import { goldenStory } from './testing/stories';

const phases: Phase[] = goldenStory('phase-markers').runs.flatMap((r) => r.phases ?? []);

describe('phaseText', () => {
	test.each([
		['complete', 'blocker wait: unblocked (uncertain)'],
		['interrupted', 'agent: interrupted: no end recorded'],
		['in-progress', 'agent: in progress'],
		['start-not-recorded', 'wiki login: ok, 1 attempt, start not recorded']
	])('a %s phase', (state, want) => {
		expect(phaseText(phases.find((p) => p.state === state)!)).toBe(want);
	});

	test('names both kinds of an inconsistent pair', () => {
		const texts = phases.filter((p) => p.state === 'inconsistent').map(phaseText);
		expect(texts).toEqual([
			'inconsistent markers: wiki login start, wiki login end',
			'inconsistent markers: wiki login start, agent end'
		]);
	});
});

test('a login’s end counts its attempts', () => {
	expect(
		endText({ kind: 'wiki-login', emitter: 'wiki_gql', at: '', outcome: 'ok', attempts: 3 })
	).toBe('ok, 3 attempts');
});

test('a phase’s first edge is the earlier, even an end before its start', () => {
	const backwards = phases.find((p) => p.state === 'inconsistent' && p.end?.kind === 'wiki-login')!;
	expect(phaseFirstAt(backwards)).toBe(backwards.end?.at);
	const late = phases.find((p) => p.state === 'start-not-recorded')!;
	expect(phaseFirstAt(late)).toBe(late.end?.at);
});
