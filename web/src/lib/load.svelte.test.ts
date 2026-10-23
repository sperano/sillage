import { flushSync } from 'svelte';
import { afterEach, expect, test } from 'vitest';
import { type Loader, load } from './load.svelte';

// The effect roots a test made, torn down after it even when it failed, so
// that none of its requests runs on into the next test.
let roots: (() => void)[] = [];

afterEach(() => {
	roots.forEach((destroy) => destroy());
	roots = [];
});

// track runs load in an effect root, as a component would, and returns
// its state and the function that tears it down early.
function track<T>(loader: () => Loader<T>) {
	let state!: ReturnType<typeof load<T>>;
	const destroy = $effect.root(() => {
		state = load(loader);
	});
	roots.push(destroy);
	flushSync();
	return { state, destroy };
}

test('nothing to load is idle', () => {
	const { state } = track(() => undefined);
	expect(state.current).toEqual({ state: 'idle' });
});

test('loads, then holds the value or the error', async () => {
	const ok = track(() => async () => 42);
	expect(ok.state.current).toEqual({ state: 'loading' });
	await expect.poll(() => ok.state.current).toEqual({ state: 'loaded', value: 42 });

	const err = new Error('down');
	const failed = track(() => async () => Promise.reject(err));
	await expect.poll(() => failed.state.current).toEqual({ state: 'failed', error: err });
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
	const { state } = track<number>(() => () => {
		throw err;
	});
	await expect.poll(() => state.current).toEqual({ state: 'failed', error: err });
});

test('a loader that throws while reading what it loads fails, and runs again on a change', async () => {
	const err = new Error('bad route');
	let broken = $state(true);
	const { state } = track(() => {
		if (broken) throw err;
		return async () => 1;
	});
	expect(state.current).toEqual({ state: 'failed', error: err });
	broken = false;
	flushSync();
	await expect.poll(() => state.current).toEqual({ state: 'loaded', value: 1 });
});

test('what the request reads is not a dependency', async () => {
	let other = $state(1);
	let calls = 0;
	track(() => async () => {
		calls++;
		return other;
	});
	other = 2;
	flushSync();
	expect(calls).toBe(1);
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
});
