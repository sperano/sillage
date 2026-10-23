import { sameItem, type ItemRef } from './timeline';

/**
 * The selected event, run, check or review wait of one story. The graph,
 * the event list and the detail panel share one instance, so selecting in
 * any of them selects in all.
 */
export class Selection {
	// A ref is replaced, never changed in place: no deep proxy.
	#current = $state.raw<ItemRef | undefined>();

	get current(): ItemRef | undefined {
		return this.#current;
	}

	select(ref: ItemRef | undefined): void {
		this.#current = ref;
	}

	isSelected(ref: ItemRef): boolean {
		return sameItem(this.current, ref);
	}
}
