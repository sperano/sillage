import { flushSync } from 'svelte';
import { expect, test } from 'vitest';
import { type Loader, load } from './load.svelte';

// track runs load in an effect root, as a component would, and returns
// its state and the function that tears it down.
function track<T>(loader: () => Loader<T>) {
	let state!: ReturnType<typeof load<T>>;
	const destroy = $effect.root(() => {
		state = load(loader);
	});
	flushSync();
	return { state, destroy };
}

test('nothing to load is idle', () => {
	const { state, destroy } = track(() => undefined);
	expect(state.current).toEqual({ state: 'idle' });
	destroy();
});

test('loads, then holds the value or the error', async () => {
	const ok = track(() => async () => 42);
	expect(ok.state.current).toEqual({ state: 'loading' });
	await expect.poll(() => ok.state.current).toEqual({ state: 'loaded', value: 42 });
	ok.destroy();

	const err = new Error('down');
	const failed = track(() => async () => Promise.reject(err));
	await expect.poll(() => failed.state.current).toEqual({ state: 'failed', error: err });
	failed.destroy();
});

test('a change of what it reads aborts the request in flight, and its late answer is dropped', async () => {
	let id = $state(1);
	const signals: AbortSignal[] = [];
	const resolvers: ((v: number) => void)[] = [];
	const { state, destroy } = track(() => {
		const wanted = id;
		return (signal) => {
			signals.push(signal);
			return new Promise<number>((resolve) => resolvers.push(() => resolve(wanted)));
		};
	});
	id = 2;
	flushSync();
	expect(signals.map((s) => s.aborted)).toEqual([true, false]);
	resolvers[0]?.(0);
	resolvers[1]?.(0);
	await expect.poll(() => state.current).toEqual({ state: 'loaded', value: 2 });
	destroy();
	expect(signals[1]?.aborted).toBe(true);
});

test('a loader that throws before its request fails instead of breaking the effect', async () => {
	const err = new Error('bad input');
	const { state, destroy } = track<number>(() => () => {
		throw err;
	});
	await expect.poll(() => state.current).toEqual({ state: 'failed', error: err });
	destroy();
});

test('what the request reads is not a dependency', async () => {
	let other = $state(1);
	let calls = 0;
	const { destroy } = track(() => async () => {
		calls++;
		return other;
	});
	other = 2;
	flushSync();
	expect(calls).toBe(1);
	destroy();
});

test('reload aborts the request in flight and loads again', async () => {
	const signals: AbortSignal[] = [];
	const loaded = track(() => async (signal) => {
		signals.push(signal);
		return signals.length;
	});
	await expect.poll(() => loaded.state.current).toEqual({ state: 'loaded', value: 1 });
	loaded.state.reload();
	flushSync();
	await expect.poll(() => loaded.state.current).toEqual({ state: 'loaded', value: 2 });
	expect(signals.map((s) => s.aborted)).toEqual([true, false]);
	loaded.destroy();
});
