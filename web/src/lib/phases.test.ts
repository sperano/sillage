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
		['start-not-recorded', 'agent: failed, start not recorded']
	])('a %s phase', (state, want) => {
		expect(phaseText(phases.find((p) => p.state === state)!)).toBe(want);
	});

	test('an end names its outcome', () => {
		expect(endText({ kind: 'agent', emitter: 'entrypoint', at: '', outcome: 'failed' })).toBe(
			'failed'
		);
	});
});

test('a phase’s first edge is its end when its start was not recorded', () => {
	const late = phases.find((p) => p.state === 'start-not-recorded')!;
	expect(phaseFirstAt(late)).toBe(late.end?.at);
});
