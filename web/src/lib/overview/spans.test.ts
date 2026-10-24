import { describe, expect, it } from 'vitest';
import { afternoon, overviewGolden, rowOfTask } from '../testing/overview';
import {
	barText,
	describeSpan,
	markLabel,
	place,
	prBars,
	runBars,
	waitBar,
	type OverviewSpan
} from './spans';

const range = { start: afternoon('14:00'), end: afternoon('15:00') };
const page = overviewGolden();

function span(fields: Partial<OverviewSpan>): OverviewSpan {
	return { key: 'k', kind: 'running', text: 'running', endState: 'known', ...fields };
}

describe('place', () => {
	it('puts a bar between its known ends', () => {
		const p = place(span({ start: afternoon('14:15'), end: afternoon('14:30') }), range);
		expect(p).toEqual({ left: 0.25, right: 0.5, clippedStart: false, clippedEnd: false });
	});

	it('clips a bar to the window', () => {
		const p = place(span({ start: afternoon('13:30'), end: afternoon('15:30') }), range);
		expect(p).toEqual({ left: 0, right: 1, clippedStart: true, clippedEnd: true });
	});

	it('draws an open end to its last read', () => {
		const s = span({ start: afternoon('14:30'), endState: 'open', openUntil: afternoon('14:45') });
		expect(place(s, range)?.right).toBe(0.75);
	});

	it('draws a stub from the only known end', () => {
		expect(place(span({ start: afternoon('14:30'), endState: 'unknown' }), range)).toMatchObject({
			left: 0.5,
			stub: 'end'
		});
		expect(place(span({ end: afternoon('14:30') }), range)).toMatchObject({
			left: 0.5,
			stub: 'start'
		});
	});

	it('leaves out what ends at the window start or is known only at its end', () => {
		expect(
			place(span({ start: afternoon('13:30'), end: afternoon('14:00') }), range)
		).toBeUndefined();
		expect(place(span({ start: afternoon('15:00'), endState: 'unknown' }), range)).toBeUndefined();
		expect(place(span({ end: afternoon('15:00') }), range)).toBeUndefined();
		expect(place(span({ end: afternoon('14:00') }), range)).toMatchObject({
			left: 0,
			stub: 'start'
		});
		expect(
			place(span({ start: afternoon('14:00'), end: afternoon('14:00') }), range)
		).toMatchObject({ left: 0, right: 0 });
	});

	it('draws nothing with no known end, or outside the window', () => {
		expect(place(span({ endState: 'unknown' }), range)).toBeUndefined();
		expect(
			place(span({ start: afternoon('13:00'), end: afternoon('13:30') }), range)
		).toBeUndefined();
		expect(place(span({ start: afternoon('13:00'), endState: 'unknown' }), range)).toBeUndefined();
	});
});

describe('the spans of the golden rows', () => {
	it('draws a run waiting to start, then running, with its exit', () => {
		const a = rowOfTask(page, 201);
		const spans = runBars(a, a.runs[0]!);
		expect(spans.map((s) => [s.kind, s.text, s.exit])).toEqual([
			['waiting', 'waiting to start', undefined],
			['running', 'running', 'run exit: succeeded']
		]);
	});

	it('draws a pull request open to its close, and a wait open to its last read', () => {
		const d = rowOfTask(page, 204);
		expect(prBars(d, d.pullRequests[0]!)).toMatchObject([
			{ kind: 'pr', start: afternoon('14:28'), end: afternoon('14:40') }
		]);
		const a = rowOfTask(page, 201);
		const wait = waitBar(a, a.reviewWaits[0]!);
		expect(wait).toMatchObject({
			kind: 'review',
			text: 'review requested: owner-gh',
			endState: 'open'
		});
		expect(wait.openUntil).toBe(Date.parse('2026-03-10T15:05:00Z'));
		expect(describeSpan(wait)).toBe(
			'review requested: owner-gh, from 2026-03-10 14:34:00 UTC, not ended at the last read, 2026-03-10 15:05:00 UTC'
		);
	});

	it('names each mark', () => {
		expect(rowOfTask(page, 204).marks.map(markLabel)).toEqual([
			'PR #23 opened',
			'PR #23 closed, not merged'
		]);
		expect(rowOfTask(page, 206).marks.map(markLabel)).toEqual([
			'PR #24 review submitted',
			'PR #24 merged',
			'task marked done'
		]);
	});
});

describe('barText', () => {
	it('says how a run ended', () => {
		const c = rowOfTask(page, 203);
		expect(runBars(c, c.runs[0]!).map(barText)).toEqual(['running · run exit: failed']);
		expect(barText(waitBar(rowOfTask(page, 201), rowOfTask(page, 201).reviewWaits[0]!))).toBe(
			'review requested: owner-gh'
		);
	});
});
