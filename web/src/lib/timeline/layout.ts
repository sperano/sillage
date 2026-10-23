// Where each row, bar and mark goes at the current zoom: bars stacked on
// tracks when they overlap, events grouped when they crowd, and labels shown
// where they fit.

import { eventKey } from '../keys';
import { eventTime, type Event } from '../model';
import { eventRef, type ItemRef } from '../timeline';
import { drawnEnd, type Span } from './intervals';
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
/** Where the row titles and the other left-column text start. */
export const TITLE_X_PX = 12;
/** The space between the two lines of a mark's label: what, then when. */
export const LABEL_LINE_GAP_PX = 12;
/** How far left of its mark a label starts. */
const LABEL_NUDGE_PX = 4;
/** How far outside the window a mark is still laid out. */
const CULL_MARGIN_PX = GROUP_GAP_PX;
/** Text cut shorter than this many characters is left out. */
const MIN_FIT_CHARS = 3;

export interface Point {
	key: string;
	event: Event;
	at: number;
	x: number;
	mark: MarkKind;
	label: string;
	selected: ItemRef;
}

/** Where a mark's label is drawn: its line of labels and its left edge. */
export interface LabelPlace {
	level: number;
	x: number;
}

export type Placed =
	| { kind: 'point'; point: Point; label?: LabelPlace }
	| {
			kind: 'group';
			id: string;
			x: number;
			members: Point[];
			expanded: boolean;
			/** The first of the row's list lines this group's list takes when expanded. */
			listLine: number;
	  };

export interface Bar {
	span: Span;
	x1: number;
	x2: number;
	/** bar: both ends placed; stub: one end unknown, dashed from the other. */
	shape: 'bar' | 'stub';
	track: number;
	/** Where the exit's label is drawn; unset when it is hidden. */
	exitLabel?: LabelPlace;
}

export interface RowLayout {
	row: Row;
	y: number;
	height: number;
	/** The vertical centre of the first track, where events sit. */
	markY: number;
	/** The baseline of the first line of labels. */
	labelY: number;
	/** The first line of the expanded groups' lists, each on its own lines. */
	listY: number;
	bars: Bar[];
	placed: Placed[];
}

/**
 * Where the span is drawn, or undefined when neither end is known. An
 * open end runs to the last read of its source; with no last read, there is
 * no time to run it to, and it is drawn as a stub like an unknown end.
 */
export function barExtent(
	span: Span,
	scale: Scale
): { x1: number; x2: number; shape: Bar['shape'] } | undefined {
	const endAt = drawnEnd(span);
	const end = endAt === undefined ? undefined : scale.x(endAt);
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
		selected: eventRef(event)
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
		return { kind: 'group', id, x, members, expanded: expanded.has(id), listLine: 0 };
	});
}

/** The width, in pixels, a mark's two lines of text take. */
export function labelWidth(...lines: string[]): number {
	return Math.max(...lines.map((l) => l.length)) * CHAR_PX;
}

/**
 * Where a label of the given width starts: just left of its mark, and
 * moved inside the plot when it would cross an edge, which clips it.
 */
function labelStart(x: number, width: number, scale: Scale): number {
	return Math.max(scale.left, Math.min(x - LABEL_NUDGE_PX, scale.right - width));
}

/**
 * Puts each label, left to right over events and bar exits together, on
 * the first line where it overlaps no label before it, and hides it when
 * none is free. A mark outside the window, laid out only for grouping,
 * has no label. A hidden label stays in the mark's accessible name and
 * tooltip. Returns the number of lines used.
 */
function placeLabels(
	placed: Placed[],
	bars: Bar[],
	scale: Scale,
	timeText: (t: number) => string
): number {
	type Slot = { x: number; width: number; show: (label: LabelPlace) => void };
	const slots: Slot[] = [];
	const slot = (markX: number, width: number, show: Slot['show']) => {
		if (markX < scale.left || markX > scale.right) return;
		slots.push({ x: labelStart(markX, width, scale), width, show });
	};
	for (const p of placed) {
		if (p.kind !== 'point') continue;
		const width = labelWidth(p.point.label, timeText(p.point.at));
		slot(p.point.x, width, (label) => (p.label = label));
	}
	for (const b of bars) {
		const exit = b.span.exit;
		if (!exit) continue;
		slot(b.x2, labelWidth(exit.label, timeText(exit.at)), (label) => (b.exitLabel = label));
	}
	const free: number[] = Array(LABEL_LEVELS).fill(Number.NEGATIVE_INFINITY);
	let used = 0;
	for (const s of slots.sort((a, b) => a.x - b.x)) {
		const level = free.findIndex((f) => s.x >= f);
		if (level < 0) continue;
		s.show({ level, x: s.x });
		free[level] = s.x + s.width + TRACK_GAP_PX;
		used = Math.max(used, level + 1);
	}
	return used;
}

/**
 * Gives each expanded group its own lines under the row's labels, left to
 * right, so that no two lists overlap. Returns the number of lines used.
 */
function stackLists(placed: Placed[]): number {
	let line = 0;
	for (const p of placed) {
		if (p.kind !== 'group' || !p.expanded) continue;
		p.listLine = line;
		line += p.members.length;
	}
	return line;
}

/** Whether x1..x2 reaches into the window, give or take the cull margin. */
function inView(x1: number, x2: number, scale: Scale): boolean {
	return x2 >= scale.left - CULL_MARGIN_PX && x1 <= scale.right + CULL_MARGIN_PX;
}

/**
 * Lays out one row from its top edge y: only what reaches into the window.
 * Bars are stacked on tracks over the whole row, drawn or not, so that
 * panning never moves a bar to another track or changes the row's tracks.
 * A span with neither end known has no extent; the row's note says so.
 */
export function layoutRow(
	row: Row,
	y: number,
	scale: Scale,
	expanded: ReadonlySet<string>,
	timeText: (t: number) => string
): RowLayout {
	const packed = packTracks(
		row.spans.flatMap((span) => {
			const extent = barExtent(span, scale);
			return extent ? [{ span, ...extent }] : [];
		})
	);
	// A loop, not Math.max(...): spreading a long array overflows the stack.
	const tracks = packed.reduce((n, b) => Math.max(n, b.track + 1), 1);
	const bars = packed.filter((b) => inView(b.x1, b.x2, scale));
	const points = row.events.map((e) => pointOf(e, scale)).filter((p) => inView(p.x, p.x, scale));
	const placed = groupPoints(points, row.id, expanded);
	const levels = Math.max(1, placeLabels(placed, bars, scale, timeText));
	const expandedLines = stackLists(placed);
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
		placed
	};
}

/** The text cut to fit width pixels, ending in an ellipsis when cut. */
export function fitText(text: string, width: number, charPx = CHAR_PX): string {
	const max = Math.floor(width / charPx);
	if (text.length <= max) return text;
	return max < MIN_FIT_CHARS ? '' : `${text.slice(0, max - 1)}…`;
}
