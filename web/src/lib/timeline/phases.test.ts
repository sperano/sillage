import { describe, expect, test } from 'vitest';
import type { Run } from '../model';
import { golden } from '../testing/goldens';
import { packPhases, phaseDescription, phaseSpans } from './phases';
import { scaleOf } from './view';

const [failed, retry] = golden('phase-markers').runs as [Run, Run];
const T0 = Date.parse('2026-03-14T10:00:00Z');
const HOUR = 3_600_000;
// Ten pixels per minute over the first attempt's hour.
const scale = scaleOf({ start: T0, end: T0 + HOUR }, 0, 600);

describe('phaseSpans', () => {
	test('a segment only for a complete phase, ticks for the rest', () => {
		expect(phaseSpans(failed).map((s) => s.shape)).toEqual([
			'segment',
			'start',
			'segment',
			'pair',
			'pair'
		]);
		expect(phaseSpans(retry).map((s) => s.shape)).toEqual(['start', 'end']);
	});

	test('a run with no phases has none', () => {
		expect(phaseSpans({ ...failed, phases: undefined })).toEqual([]);
	});
});

describe('packPhases', () => {
	test('a login inside the agent span goes on another sub-track', () => {
		const placed = packPhases(phaseSpans(failed), scale);
		const track = (state: string, kind: string) =>
			placed.find((p) => p.span.phase.state === state && p.span.phase.start?.kind === kind)?.track;
		expect(track('interrupted', 'agent')).not.toBe(track('complete', 'wiki-login'));
	});

	test('ends a tick’s room after its text, so that no two overlap on a sub-track', () => {
		const placed = packPhases(phaseSpans(failed), scale);
		for (const a of placed) {
			for (const b of placed) {
				if (a === b || a.track !== b.track) continue;
				expect(a.right <= b.left || b.right <= a.left).toBe(true);
			}
		}
	});
});

test('a phase’s description says what is not recorded and who reported it', () => {
	const [progress, late] = phaseSpans(retry);
	expect(phaseDescription(progress!, 'utc')).toContain('not ended at the last read');
	expect(phaseDescription(late!, 'utc')).toContain('start not recorded');
	expect(phaseDescription(late!, 'utc')).toContain('reported by wiki_gql in pod');
});
