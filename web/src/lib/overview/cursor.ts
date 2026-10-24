// The overview's cursor: a whole number of minutes from the window's
// start, which the slider steps through and a click on a lane snaps to,
// so that both put it at the same places.

import { MINUTE_MS } from '../timeline/time';
import type { Range } from './spans';

/**
 * The cursor's last place: the last minute of the window counted from its
 * start, or the start of a window shorter than two minutes.
 */
export function lastCursor(range: Range): number {
	const minutes = Math.floor((range.end - range.start) / MINUTE_MS);
	return range.start + Math.max(0, minutes - 1) * MINUTE_MS;
}

/** The cursor's place nearest t. */
export function snapCursor(range: Range, t: number): number {
	const at = range.start + Math.round((t - range.start) / MINUTE_MS) * MINUTE_MS;
	return Math.min(lastCursor(range), Math.max(range.start, at));
}
