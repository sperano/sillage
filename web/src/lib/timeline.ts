import type { CheckRun, Event, Phase, PRKey, PullRequest, Run, Story } from './model';
import { phaseFirstAt } from './phases';
import { eventKey, prKeyString, runKeyString } from './keys';
import { intervalAnchor, parseTime } from './time';

/**
 * What can be selected on the timeline: an event, a run, a check, a review
 * wait or a run's phase. Events and runs use the ids correlation refs
 * carry, so a correlation's end can be selected; a check has no ref and
 * gets its pull request's key and id; a review wait, drawn only by the
 * graph, gets the key of the review request it starts at; a phase, its
 * run's key, its pod's UID and its span.
 */
export interface ItemRef {
	kind: 'event' | 'run' | 'check' | 'review-wait' | 'phase';
	id: string;
}

interface Placed {
	ref: ItemRef;
	/** Where the item sits in time; undefined when no bound is known. */
	time?: string;
}

export type TimelineItem =
	| (Placed & { kind: 'event'; event: Event })
	| (Placed & { kind: 'run'; run: Run })
	| (Placed & { kind: 'check'; pr: PullRequest; check: CheckRun })
	| (Placed & { kind: 'phase'; run: Run; phase: Phase });

export function eventRef(e: Event): ItemRef & { kind: 'event' } {
	return { kind: 'event', id: eventKey(e) };
}

export function runRef(r: Run): ItemRef & { kind: 'run' } {
	return { kind: 'run', id: runKeyString(r.key) };
}

export function checkRef(pr: PRKey, c: CheckRun): ItemRef & { kind: 'check' } {
	return { kind: 'check', id: `${prKeyString(pr)}/checks/${c.id}` };
}

export function phaseRef(r: Run, p: Phase): ItemRef & { kind: 'phase' } {
	return { kind: 'phase', id: `${runKeyString(r.key)}/phases/${p.podUID}/${p.span}` };
}

export function reviewWaitRef(request: Event): ItemRef & { kind: 'review-wait' } {
	return { kind: 'review-wait', id: eventKey(request) };
}

export function sameItem(a: ItemRef | undefined, b: ItemRef | undefined): boolean {
	return a !== undefined && b !== undefined && a.kind === b.kind && a.id === b.id;
}

/**
 * Every event, run, check and phase of the story in chronological order:
 * an event at its source time, else when it was observed; a run or check
 * at its first known bound; a phase at its first recorded edge. Those with no known bound come last, since nothing
 * places them in time. Equal times keep the story's order, events first.
 */
export function timelineItems(story: Story): TimelineItem[] {
	const items: TimelineItem[] = [
		...story.events.map((event) => ({
			kind: 'event' as const,
			ref: eventRef(event),
			time: event.sourceTime ?? event.observedAt,
			event
		})),
		...story.runs.map((run) => ({
			kind: 'run' as const,
			ref: runRef(run),
			time: intervalAnchor(run.interval),
			run
		})),
		...story.pullRequests.flatMap((pr) =>
			pr.checks.map((check) => ({
				kind: 'check' as const,
				ref: checkRef(pr.key, check),
				time: intervalAnchor(check.interval),
				pr,
				check
			}))
		),
		...story.runs.flatMap((run) =>
			(run.phases ?? []).map((phase) => ({
				kind: 'phase' as const,
				ref: phaseRef(run, phase),
				time: phaseFirstAt(phase),
				run,
				phase
			}))
		)
	];
	return items.sort(byTime);
}

/** Unplaced items, and any whose time does not parse, sort last. */
function byTime(a: TimelineItem, b: TimelineItem): number {
	const at = a.time === undefined ? undefined : parseTime(a.time);
	const bt = b.time === undefined ? undefined : parseTime(b.time);
	if (at === undefined || bt === undefined) {
		return Number(at === undefined) - Number(bt === undefined);
	}
	return at - bt;
}

export function findItem(items: readonly TimelineItem[], ref: ItemRef): TimelineItem | undefined {
	return items.find((i) => sameItem(i.ref, ref));
}
