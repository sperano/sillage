import { describe, expect, test } from 'vitest';
import { eventKey } from '../model';
import { event } from '../testing/builders';
import type { Span } from './intervals';
import type { Row } from './lanes';
import {
	barExtent,
	fitText,
	GROUP_GAP_PX,
	groupPoints,
	layoutRow,
	MIN_BAR_PX,
	STUB_PX,
	type Point
} from './layout';
import { scaleOf } from './view';

const T0 = Date.parse('2026-03-01T10:00:00Z');
const MINUTE = 60_000;
// One pixel per minute, from 0 to 100.
const scale = scaleOf({ start: T0, end: T0 + 100 * MINUTE }, 0, 100);

function span(over: Partial<Span>): Span {
	return {
		key: 'run:r',
		selected: { kind: 'run', key: 'r' },
		phase: 'running',
		text: 'running',
		startState: 'known',
		start: T0 + 10 * MINUTE,
		endState: 'known',
		end: T0 + 20 * MINUTE,
		...over
	};
}

describe('barExtent', () => {
	test('draws a bar only when both ends are placed', () => {
		expect(barExtent(span({}), scale)).toEqual({ x1: 10, x2: 20, shape: 'bar' });
	});

	test('runs an open end to its last read, and draws a stub without one', () => {
		expect(
			barExtent(span({ endState: 'open', end: undefined, openUntil: T0 + 50 * MINUTE }), scale)
		).toMatchObject({ x2: 50, shape: 'bar' });
		expect(barExtent(span({ endState: 'open', end: undefined }), scale)).toEqual({
			x1: 10,
			x2: 10 + STUB_PX,
			shape: 'stub'
		});
	});

	test('draws a stub from the known end when the other is unknown', () => {
		expect(barExtent(span({ endState: 'unknown', end: undefined }), scale)).toEqual({
			x1: 10,
			x2: 10 + STUB_PX,
			shape: 'stub'
		});
		expect(barExtent(span({ startState: 'unknown', start: undefined }), scale)).toEqual({
			x1: 20 - STUB_PX,
			x2: 20,
			shape: 'stub'
		});
	});

	test('draws nothing when neither end is known', () => {
		expect(
			barExtent(
				span({ startState: 'unknown', start: undefined, endState: 'unknown', end: undefined }),
				scale
			)
		).toBeUndefined();
	});

	test('keeps a short bar visible', () => {
		expect(barExtent(span({ end: T0 + 10 * MINUTE }), scale)!.x2).toBe(10 + MIN_BAR_PX);
	});
});

function point(x: number): Point {
	const e = event();
	return {
		key: eventKey(e),
		event: e,
		at: scale.time(x),
		x,
		mark: 'event',
		label: 'comment',
		selected: { kind: 'event', key: eventKey(e) }
	};
}

describe('groupPoints', () => {
	test('groups points closer than the gap, with a stable id, and expands on request', () => {
		const [a, b, c] = [point(10), point(10 + GROUP_GAP_PX - 1), point(60)];
		const placed = groupPoints([c!, b!, a!], 'row', new Set([`row|${a!.key}`]));
		expect(placed).toHaveLength(2);
		expect(placed[0]).toMatchObject({ kind: 'group', id: `row|${a!.key}`, expanded: true });
		expect(placed[0]!.kind === 'group' && placed[0]!.members).toEqual([a, b]);
		expect(placed[1]).toMatchObject({ kind: 'point', point: c });
	});
});

describe('layoutRow', () => {
	const row = (spans: Span[], events = [] as Row['events']): Row => ({
		id: 'r',
		section: 'runs',
		title: 't',
		subtitle: 's',
		sources: [],
		events,
		spans,
		order: 0
	});

	test('stacks overlapping bars on separate tracks', () => {
		const laid = layoutRow(
			row([
				span({}),
				span({ start: T0 + 15 * MINUTE, end: T0 + 30 * MINUTE }),
				span({ start: T0 + 40 * MINUTE, end: T0 + 45 * MINUTE })
			]),
			0,
			scale,
			new Set(),
			String
		);
		expect(laid.bars.map((b) => b.track)).toEqual([0, 1, 0]);
	});

	test('keeps a span with no known end out of the bars', () => {
		const unknown = span({
			startState: 'unknown',
			start: undefined,
			endState: 'unknown',
			end: undefined
		});
		expect(layoutRow(row([unknown]), 0, scale, new Set(), String).bars).toEqual([]);
	});

	test('lays out only what reaches into the window', () => {
		const before = span({ start: T0 - 50 * MINUTE, end: T0 - 40 * MINUTE });
		const across = span({ start: T0 - 50 * MINUTE, end: T0 + 5 * MINUTE });
		const events = [
			event({ sourceTime: new Date(T0 + 200 * MINUTE).toISOString() }),
			event({ sourceTime: new Date(T0 + 50 * MINUTE).toISOString() })
		];
		const laid = layoutRow(row([before, across], events), 0, scale, new Set(), String);
		expect(laid.bars.map((b) => b.span)).toEqual([across]);
		expect(laid.placed.map((p) => p.kind === 'point' && p.point.event)).toEqual([events[1]]);
	});

	test('moves a label that would overlap down a line, and hides it when no line is free', () => {
		const events = [
			event({ sourceTime: new Date(T0 + 10 * MINUTE).toISOString() }),
			event({ sourceTime: new Date(T0 + 10 * MINUTE + (GROUP_GAP_PX + 1) * MINUTE).toISOString() }),
			event({
				sourceTime: new Date(T0 + 10 * MINUTE + 2 * (GROUP_GAP_PX + 1) * MINUTE).toISOString()
			})
		];
		const laid = layoutRow(row([], events), 0, scale, new Set(), () => '10:00:00');
		expect(laid.placed.map((p) => p.kind === 'point' && p.labelLevel)).toEqual([0, 1, undefined]);
	});
});

test('fitText cuts with an ellipsis, and drops what would be too short', () => {
	expect(fitText('abcdef', 100)).toBe('abcdef');
	expect(fitText('abcdefghijklmnopqrstuvwxyz', 62)).toBe('abcdefghi…');
	expect(fitText('abcdefghij', 6)).toBe('');
});
