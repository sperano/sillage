// A run's phases on its row: each span its pod reported, drawn on a
// sub-track under the run's bar. A segment only when both ends are
// recorded and pair; otherwise ticks at the recorded edges, with what is
// missing or wrong. Overlapping phases (a login inside the agent) stack
// on sub-tracks; a run with no phases has none.

import { runKeyString } from '../keys';
import type { Phase, Run } from '../model';
import { firstEdge, kindName, phaseKind, phaseText, stateText } from '../phases';
import { phaseRef, type ItemRef } from '../timeline';
import { formatFull, type Zone } from './time';
import type { Scale } from './view';

/** The height of one sub-track of phases. */
export const PHASE_TRACK_PX = 18;
/** The narrowest a segment is drawn. */
export const MIN_SEGMENT_PX = 3;
/** The space kept between two phases on one sub-track. */
const PHASE_GAP_PX = 4;
/** How far a tick's text starts from its tick. */
export const TICK_TEXT_PAD_PX = 4;
/** The room a chevron takes after an in-progress phase's tick. */
export const CHEVRON_ROOM_PX = 8;
/** Approximate width of one character of a tick's 10px text. */
const TICK_CHAR_PX = 5.6;

/**
 * How a phase is drawn: a segment between its recorded ends, a tick at its
 * start (in progress or interrupted), a tick at its end (start not
 * recorded), or a tick at each edge (inconsistent).
 */
export type PhaseShape = 'segment' | 'start' | 'end' | 'pair';

export interface PhaseSpan {
	key: string;
	selected: ItemRef;
	phase: Phase;
	shape: PhaseShape;
	/** The times of the recorded edges, by the pod's clock. */
	start?: number;
	end?: number;
	/** The segment's text, or what a tick says. */
	text: string;
	uncertain: boolean;
}

function shapeOf(p: Phase): PhaseShape {
	switch (p.state) {
		case 'complete':
			return 'segment';
		case 'start-not-recorded':
			return 'end';
		case 'inconsistent':
			return 'pair';
		case 'in-progress':
		case 'interrupted':
			return 'start';
	}
}

/** The phases of one run, as its row draws them. */
export function phaseSpans(run: Run): PhaseSpan[] {
	return (run.phases ?? []).map((phase) => {
		const shape = shapeOf(phase);
		return {
			key: `phase:${runKeyString(run.key)}/${phase.podUID}/${phase.span}`,
			selected: phaseRef(run, phase),
			phase,
			shape,
			start: phase.start && Date.parse(phase.start.at),
			end: phase.end && Date.parse(phase.end.at),
			text: shape === 'segment' ? phaseText(phase) : tickText(phase),
			uncertain: phase.confidence === 'heuristic'
		};
	});
}

/** What a phase drawn as ticks says next to them: its kind and its state. */
function tickText(p: Phase): string {
	const kind = kindName(phaseKind(p));
	const uncertain = p.confidence === 'heuristic' ? ', uncertain' : '';
	return p.state === 'inconsistent'
		? `${stateText(p.state)}${uncertain}`
		: `${kind}: ${stateText(p.state)}${uncertain}`;
}

/** The width a tick's text takes. */
export function tickTextWidth(span: PhaseSpan): number {
	return span.text.length * TICK_CHAR_PX;
}

export interface PlacedPhase {
	span: PhaseSpan;
	/** Where the segment or the ticks are drawn. */
	x1: number;
	x2: number;
	/** The extent with a tick's text, which no other phase on its sub-track overlaps. */
	left: number;
	right: number;
	track: number;
}

/** Where a phase is drawn, and the room it takes with its text. */
function extent(span: PhaseSpan, scale: Scale): Omit<PlacedPhase, 'track'> {
	const xs = [span.start, span.end].filter((t) => t !== undefined).map((t) => scale.x(t));
	const x1 = Math.min(...xs);
	if (span.shape === 'segment') {
		const x2 = Math.max(Math.max(...xs), x1 + MIN_SEGMENT_PX);
		return { span, x1, x2, left: x1, right: x2 };
	}
	const x2 = Math.max(...xs);
	const text = TICK_TEXT_PAD_PX + tickTextWidth(span);
	if (span.shape === 'end') return { span, x1, x2, left: x1 - text, right: x2 };
	const chevron = span.phase.state === 'in-progress' ? CHEVRON_ROOM_PX : 0;
	return { span, x1, x2, left: x1, right: x2 + chevron + text };
}

/**
 * Assigns each phase the first sub-track on which it overlaps no other,
 * over the whole row, so that panning never moves a phase to another
 * sub-track.
 */
export function packPhases(spans: readonly PhaseSpan[], scale: Scale): PlacedPhase[] {
	const trackEnds: number[] = [];
	return spans
		.map((s) => extent(s, scale))
		.sort((a, b) => a.left - b.left || a.span.key.localeCompare(b.span.key))
		.map((p) => {
			let track = trackEnds.findIndex((end) => end + PHASE_GAP_PX <= p.left);
			if (track < 0) track = trackEnds.length;
			trackEnds[track] = p.right;
			return { ...p, track };
		});
}

/** Everything a phase shows, in words: what it is, its recorded edges and who reported them. */
export function phaseDescription(span: PhaseSpan, zone: Zone): string {
	const { phase } = span;
	const at = (t: number | undefined, missing: string) =>
		t === undefined ? missing : formatFull(t, zone);
	const end = phase.state === 'in-progress' ? 'not ended at the last read' : 'end not recorded';
	const emitter = firstEdge(phase).emitter;
	return [
		`Phase ${phaseText(phase)}`,
		`start ${at(span.start, 'not recorded')}`,
		span.end === undefined ? end : `end ${at(span.end, '')}`,
		`reported by ${emitter} in pod ${phase.pod}`
	].join(', ');
}
