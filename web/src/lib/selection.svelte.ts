// The selected event or interval, shared by the timeline graph, the event
// list and the detail panel: selecting in one selects in all.

/** What can be selected: an event, a run, a check run, or a review wait. */
export type Selected =
	| { kind: 'event'; key: string }
	| { kind: 'run'; key: string }
	| { kind: 'check'; pr: string; id: number }
	| { kind: 'review-wait'; key: string };

/** A string identifying the selected record, equal for equal selections. */
export function selectedId(s: Selected): string {
	switch (s.kind) {
		case 'check':
			return `check:${s.pr}#${s.id}`;
		default:
			return `${s.kind}:${s.key}`;
	}
}

/** The current selection; one instance is shared by every view of a story. */
export class Selection {
	current = $state<Selected | null>(null);

	select(s: Selected): void {
		this.current = s;
	}

	clear(): void {
		this.current = null;
	}

	/** Whether s is the current selection. */
	is(s: Selected): boolean {
		return this.current !== null && selectedId(this.current) === selectedId(s);
	}
}
