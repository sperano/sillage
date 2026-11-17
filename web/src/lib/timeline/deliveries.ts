// The Sillage lane: the webhook deliveries sillage received for the story's
// root, one small mark each, drawn only when the window is narrow enough
// for them not to crowd the rest; wider, the lane only counts them.

import type { Delivery } from '../decodeApi';
import { count } from '../format';
import type { Row } from './lanes';
import { HOUR_MS } from './time';

/** The widest window at which the lane draws its deliveries. */
export const DELIVERY_MARKS_MAX_SPAN_MS = 2 * HOUR_MS;

export const DELIVERY_ROW_ID = 'sillage:deliveries';

/** Whether a window spanning spanMs draws the deliveries' marks. */
export function deliveriesShown(spanMs: number): boolean {
	return spanMs <= DELIVERY_MARKS_MAX_SPAN_MS;
}

/**
 * The lane's one row, drawing the deliveries in a window spanning spanMs,
 * or saying how many it hides in a wider one; none without deliveries.
 */
export function deliveryRow(deliveries: readonly Delivery[], spanMs: number): Row | undefined {
	if (deliveries.length === 0) return undefined;
	const shown = deliveriesShown(spanMs);
	return {
		id: DELIVERY_ROW_ID,
		section: 'sillage',
		title: 'Webhook deliveries',
		subtitle: 'received by sillage',
		sources: [],
		events: [],
		spans: [],
		phases: [],
		deliveries: shown ? [...deliveries] : [],
		note: shown
			? undefined
			: `${count(deliveries.length, 'delivery', 'deliveries')} · zoom in to see them`,
		order: 0
	};
}
