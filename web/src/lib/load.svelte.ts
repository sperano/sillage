// A request a component makes for what it shows, aborted when what it
// shows changes or the component goes away: a navigation never leaves a
// request running, nor lets a late answer overwrite a newer one.

import { untrack } from 'svelte';

export type Load<T> =
	/** Nothing to load. */
	| { state: 'idle' }
	| { state: 'loading' }
	| { state: 'loaded'; value: T }
	| { state: 'failed'; error: unknown };

/** A function that loads with signal, or undefined when there is nothing to load. */
export type Loader<T> = ((signal: AbortSignal) => Promise<T>) | undefined;

/**
 * Runs loader in an effect, from a component's initialization. The state
 * loader reads when called (props, route), and only that, is the effect's
 * dependencies: when one changes, the request in flight is aborted and
 * loader runs again.
 */
export function load<T>(loader: () => Loader<T>): { readonly current: Load<T> } {
	let current: Load<T> = $state.raw({ state: 'idle' });
	$effect(() => {
		const run = loader();
		if (run === undefined) {
			current = { state: 'idle' };
			return;
		}
		const controller = new AbortController();
		current = { state: 'loading' };
		const settle = (next: Load<T>) => {
			if (!controller.signal.aborted) current = next;
		};
		// The request runs untracked: what it reads before its first await is
		// not a dependency. The async wrapper turns a synchronous throw into a
		// failure.
		void untrack(async () => run(controller.signal)).then(
			(value) => settle({ state: 'loaded', value }),
			(error: unknown) => settle({ state: 'failed', error })
		);
		return () => controller.abort();
	});
	return {
		get current() {
			return current;
		}
	};
}
