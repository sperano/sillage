// Where each row, bar and mark goes at the current zoom: bars stacked on
// tracks when they overlap, events grouped when they crowd, and labels shown
// where they fit.

import { eventKey, eventTime, type Event } from '../model';
import type { Selected } from '../selection.svelte';
import type { Span } from './intervals';
import type { Row } from './lanes';
import { eventLabel, eventMark, type MarkKind } from './marks';
import type { Scale } from './view';

/** Events closer than this, in pixels, are grouped under one count. */
export const GROUP_GAP_PX = 14;
/** The length of the dashed stub drawn from an interval's known end. */
export const STUB_PX = 28;
/** The narrowest a bar is drawn, so that a short interval stays visible. */
export const MIN_BAR_PX = 3;
/** The height of one track of bars and marks. */
export const TRACK_PX = 26;
/** The room under the tracks for one line of marks' text (two lines each). */
export const LABEL_BAND_PX = 30;
/** The lines of labels a row tries before hiding a label. */
const LABEL_LEVELS = 2;
/** How far under the last track the first label's baseline sits. */
const LABEL_OFFSET_PX = 10;
/** The height of one line of an expanded group. */
export const LINE_PX = 15;
/** Space above the first track. */
export const ROW_PAD_PX = 6;
/** The space kept between two bars on one track, and between two labels. */
const TRACK_GAP_PX = 4;
/** Approximate width of one character of mark text, to keep labels apart. */
export const CHAR_PX = 6.2;
/** The same for the 10px text of subtitles, notes and coverage reasons. */
export const SMALL_CHAR_PX = 5.6;
/** Text cut shorter than this many characters is left out. */
const MIN_FIT_CHARS = 3;

export interface Point {
	key: string;
	event: Event;
	at: number;
	x: number;
	mark: MarkKind;
	label: string;
	selected: Selected;
}

export type Placed =
	| { kind: 'point'; point: Point; labelLevel?: number }
	| { kind: 'group'; id: string; x: number; members: Point[]; expanded: boolean };

export interface Bar {
	span: Span;
	x1: number;
	x2: number;
	/** bar: both ends placed; stub: one end unknown, dashed from the other. */
	shape: 'bar' | 'stub';
	track: number;
	/** The line of labels the exit's label is on; unset when it is hidden. */
	exitLabelLevel?: number;
}

export interface RowLayout {
	row: Row;
	y: number;
	height: number;
	/** The vertical centre of the first track, where events sit. */
	markY: number;
	/** The baseline of the first line of labels. */
	labelY: number;
	/** Where expanded groups list their members. */
	listY: number;
	bars: Bar[];
	placed: Placed[];
	/** Spans with neither end known: listed, never drawn. */
	undrawn: Span[];
}

/**
 * Where the span is drawn, or undefined when neither end is known. An
 * open end runs to the last read of its source, else to the window's end.
 */
export function barExtent(
	span: Span,
	scale: Scale
): { x1: number; x2: number; shape: Bar['shape'] } | undefined {
	const end =
		span.endState === 'open'
			? scale.x(span.openUntil ?? scale.window.end)
			: span.end === undefined
				? undefined
				: scale.x(span.end);
	if (span.start !== undefined && end !== undefined) {
		const x1 = scale.x(span.start);
		return { x1, x2: Math.max(end, x1 + MIN_BAR_PX), shape: 'bar' };
	}
	if (span.start !== undefined) {
		const x1 = scale.x(span.start);
		return { x1, x2: x1 + STUB_PX, shape: 'stub' };
	}
	if (end !== undefined) return { x1: end - STUB_PX, x2: end, shape: 'stub' };
	return undefined;
}

/** Assigns each bar the first track on which it overlaps no other. */
function packTracks(bars: Omit<Bar, 'track'>[]): Bar[] {
	const trackEnds: number[] = [];
	return [...bars]
		.sort((a, b) => a.x1 - b.x1)
		.map((bar) => {
			let track = trackEnds.findIndex((end) => end + TRACK_GAP_PX <= bar.x1);
			if (track < 0) track = trackEnds.length;
			trackEnds[track] = bar.x2;
			return { ...bar, track };
		});
}

function pointOf(event: Event, scale: Scale): Point {
	const key = eventKey(event);
	const at = eventTime(event);
	return {
		key,
		event,
		at,
		x: scale.x(at),
		mark: eventMark(event),
		label: eventLabel(event),
		selected: { kind: 'event', key }
	};
}

/** Groups points closer than GROUP_GAP_PX to their neighbour. */
export function groupPoints(
	points: Point[],
	rowId: string,
	expanded: ReadonlySet<string>
): Placed[] {
	const sorted = [...points].sort((a, b) => a.x - b.x || a.key.localeCompare(b.key));
	const clusters: Point[][] = [];
	for (const p of sorted) {
		const last = clusters[clusters.length - 1];
		const prev = last?.[last.length - 1];
		if (last && prev && p.x - prev.x < GROUP_GAP_PX) last.push(p);
		else clusters.push([p]);
	}
	return clusters.map((members): Placed => {
		if (members.length === 1) return { kind: 'point', point: members[0]! };
		const id = `${rowId}|${members[0]!.key}`;
		const x = members.reduce((sum, m) => sum + m.x, 0) / members.length;
		return { kind: 'group', id, x, members, expanded: expanded.has(id) };
	});
}

/** The width, in pixels, a mark's two lines of text take. */
export function labelWidth(...lines: string[]): number {
	return Math.max(...lines.map((l) => l.length)) * CHAR_PX;
}

/**
 * Puts each label, left to right over events and bar exits together, on
 * the first line where it overlaps no label before it, and hides it when
 * none is free. A hidden label stays in the mark's accessible name and
 * tooltip. Returns the number of lines used.
 */
function placeLabels(placed: Placed[], bars: Bar[], timeText: (t: number) => string): number {
	type Slot = { x: number; width: number; show: (level: number) => void };
	const slots: Slot[] = [];
	for (const p of placed) {
		if (p.kind !== 'point') continue;
		const width = labelWidth(p.point.label, timeText(p.point.at));
		slots.push({ x: p.point.x, width, show: (level) => (p.labelLevel = level) });
	}
	for (const b of bars) {
		const exit = b.span.exit;
		if (!exit) continue;
		const width = labelWidth(exit.label, timeText(exit.at));
		slots.push({ x: b.x2, width, show: (level) => (b.exitLabelLevel = level) });
	}
	const free: number[] = Array(LABEL_LEVELS).fill(Number.NEGATIVE_INFINITY);
	let used = 0;
	for (const slot of slots.sort((a, b) => a.x - b.x)) {
		const level = free.findIndex((f) => slot.x >= f);
		if (level < 0) continue;
		slot.show(level);
		free[level] = slot.x + slot.width + TRACK_GAP_PX;
		used = Math.max(used, level + 1);
	}
	return used;
}

/** Lays out one row from its top edge y. */
export function layoutRow(
	row: Row,
	y: number,
	scale: Scale,
	expanded: ReadonlySet<string>,
	timeText: (t: number) => string
): RowLayout {
	const undrawn: Span[] = [];
	const unpacked = row.spans.flatMap((span) => {
		const extent = barExtent(span, scale);
		if (!extent) undrawn.push(span);
		return extent ? [{ span, ...extent }] : [];
	});
	const bars = packTracks(unpacked);
	const tracks = Math.max(1, ...bars.map((b) => b.track + 1));
	const placed = groupPoints(
		row.events.map((e) => pointOf(e, scale)),
		row.id,
		expanded
	);
	const levels = Math.max(1, placeLabels(placed, bars, timeText));
	// Each expanded group lists its members under itself, from the same line.
	const expandedLines = Math.max(
		0,
		...placed.map((p) => (p.kind === 'group' && p.expanded ? p.members.length : 0))
	);
	const tracksBottom = y + ROW_PAD_PX + tracks * TRACK_PX;
	const listY = tracksBottom + levels * LABEL_BAND_PX;
	return {
		row,
		y,
		height: listY - y + expandedLines * LINE_PX,
		markY: y + ROW_PAD_PX + TRACK_PX / 2,
		labelY: tracksBottom + LABEL_OFFSET_PX,
		listY,
		bars,
		placed,
		undrawn
	};
}

/** The text cut to fit width pixels, ending in an ellipsis when cut. */
export function fitText(text: string, width: number, charPx = CHAR_PX): string {
	const max = Math.floor(width / charPx);
	if (text.length <= max) return text;
	return max < MIN_FIT_CHARS ? '' : `${text.slice(0, max - 1)}…`;
}
