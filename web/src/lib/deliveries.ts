// The webhook deliveries sillage received for a story's root, and what the
// panels and the graph say of them. A delivery is evidence that sillage
// received it, and of nothing else: no record of a story names a delivery
// id, so none is ever linked to an event; one is only listed near it, by
// time.

import type { Delivery, DeliveryOutcome } from './decodeApi';
import { SOURCE_NAMES } from './format';
import type { Event, EventKind } from './model';
import { parseTime } from './time';
import type { ItemRef } from './timeline';

/** How far from a state change, either way, the deliveries listed with it were received. */
export const NEARBY_MINUTES = 30;
const MS_PER_MINUTE = 60_000;
const NEARBY_MS = NEARBY_MINUTES * MS_PER_MINUTE;

/** What the panels say every delivery is, and is not, evidence of. */
export const RECEIPT_CAVEAT =
	'Received by sillage: not evidence that Kelos received its own delivery, nor that it caused anything.';

/**
 * The state changes whose detail lists the deliveries received around
 * them: a task done or seen in a state, a pull request merged, closed or
 * reopened.
 */
export const STATE_CHANGES: ReadonlySet<EventKind> = new Set<EventKind>([
	'task.done',
	'task.completion_observed',
	'task.bucket_observed',
	'task.label_observed',
	'task.assignee_observed',
	'pr.merged',
	'pr.closed',
	'pr.reopened'
]);

/**
 * One attempt's identity: a redelivery reuses the delivery id, so the
 * attempt is the id and the time it was received.
 */
export function deliveryKey(d: Delivery): string {
	return `${d.source}/${d.instance}/${d.id}@${d.receivedAt}`;
}

export function deliveryRef(d: Delivery): ItemRef & { kind: 'delivery' } {
	return { kind: 'delivery', id: deliveryKey(d) };
}

export function findDelivery(deliveries: readonly Delivery[], ref: ItemRef): Delivery | undefined {
	return ref.kind === 'delivery' ? deliveries.find((d) => deliveryKey(d) === ref.id) : undefined;
}

/** "GitHub pull_request": the source and the event it named. */
export function deliveryLabel(d: Delivery): string {
	return `${SOURCE_NAMES[d.source]} ${d.event ?? 'delivery (event unknown)'}`;
}

const OUTCOME_TEXT: Readonly<Record<DeliveryOutcome, string>> = {
	accepted: 'accepted: sillage asked to read the task again',
	ignored: 'ignored: sillage asked for no read',
	rejected: 'rejected: sillage could not read the payload',
	failed: 'failed: sillage could not ask for every read'
};

/** What sillage did with the attempt, and why when it says. */
export function outcomeText(d: Delivery): string {
	const text = OUTCOME_TEXT[d.outcome];
	return d.detail ? `${text} (${d.detail})` : text;
}

/** When the delivery was received, in milliseconds; NaN for a time that does not parse. */
export function receivedTime(d: Delivery): number {
	return parseTime(d.receivedAt) ?? Number.NaN;
}

/** Whether selecting the event lists the deliveries received around it. */
export function isStateChange(e: Event): boolean {
	return STATE_CHANGES.has(e.kind);
}

/**
 * The deliveries received within NEARBY_MINUTES of at, either way, in the
 * order received: listed by time alone, never as a cause.
 */
export function deliveriesAround(deliveries: readonly Delivery[], at: number): Delivery[] {
	return deliveries.filter((d) => Math.abs(receivedTime(d) - at) <= NEARBY_MS);
}
