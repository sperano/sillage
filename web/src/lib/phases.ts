// Plain words for a run's phases: what the timeline, the event list and
// the detail panel say of the spans a pod reported with phase markers.

import { compareTimes } from './check';
import type { MarkerKind, Phase, PhaseMark, PhaseState } from './model';
import { named } from './named';

const kindNames: Record<MarkerKind, string> = {
	'blocker-wait': 'blocker wait',
	'wiki-login': 'wiki login',
	agent: 'agent'
};

/** A marker kind in words. */
export function kindName(kind: MarkerKind): string {
	return named(kindNames, kind);
}

/** What a phase with no interval to draw says at its ticks, by state. */
const stateTexts: Record<PhaseState, string> = {
	complete: 'complete',
	'in-progress': 'in progress',
	interrupted: 'interrupted: no end recorded',
	'start-not-recorded': 'start not recorded',
	inconsistent: 'inconsistent markers'
};

/** A phase's state in words: what its ticks say when it has no segment. */
export function stateText(state: PhaseState): string {
	return named(stateTexts, state);
}

/** The phase's kind: its start's, else its end's. */
export function phaseKind(p: Phase): MarkerKind {
	// A decoded phase has at least one edge.
	return (p.start ?? p.end)!.kind;
}

/** How an end edge says its span ended: its outcome and, for a login, its attempts. */
export function endText(mark: PhaseMark): string {
	const attempts =
		mark.attempts === undefined
			? ''
			: `, ${mark.attempts} ${mark.attempts === 1 ? 'attempt' : 'attempts'}`;
	return `${mark.outcome ?? 'no outcome'}${attempts}`;
}

/**
 * What the phase is, in a few words: its kind, and its outcome when its end
 * pairs with its start, or what is missing or wrong; "uncertain" when it
 * is joined to its run by Task name and time, not by its pod.
 */
export function phaseText(p: Phase): string {
	const uncertain = p.confidence === 'heuristic' ? ' (uncertain)' : '';
	return `${phaseTitle(p)}${uncertain}`;
}

function phaseTitle(p: Phase): string {
	const kind = kindName(phaseKind(p));
	switch (p.state) {
		case 'complete':
			return `${kind}: ${endText(p.end!)}`;
		case 'start-not-recorded':
			return `${kind}: ${endText(p.end!)}, start not recorded`;
		case 'inconsistent':
			return `inconsistent markers: ${kindName(p.start!.kind)} start, ${kindName(p.end!.kind)} end`;
		default:
			return `${kind}: ${stateText(p.state)}`;
	}
}

/** The time of the phase's earlier recorded edge, as sent. */
export function phaseFirstAt(p: Phase): string {
	const { start, end } = p;
	if (!start) return end!.at;
	if (!end) return start.at;
	return compareTimes(end.at, start.at) < 0 ? end.at : start.at;
}
