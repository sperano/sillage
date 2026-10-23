import { sameItem, type ItemRef } from './timeline';

/**
 * The selected event, run or check of one story. The graph, the event list
 * and the detail panel share one instance, so selecting in any of them
 * selects in all.
 */
export class Selection {
	current = $state<ItemRef | undefined>();

	select(ref: ItemRef | undefined): void {
		this.current = ref;
	}

	isSelected(ref: ItemRef): boolean {
		return sameItem(this.current, ref);
	}
}
