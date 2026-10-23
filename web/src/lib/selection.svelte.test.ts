import { expect, test } from 'vitest';
import { Selection, selectedId } from './selection.svelte';

test('selects, compares by identity and clears', () => {
	const selection = new Selection();
	expect(selection.current).toBeNull();
	selection.select({ kind: 'check', pr: 'p', id: 1 });
	expect(selection.is({ kind: 'check', pr: 'p', id: 1 })).toBe(true);
	expect(selection.is({ kind: 'check', pr: 'p', id: 2 })).toBe(false);
	expect(selection.is({ kind: 'event', key: 'p' })).toBe(false);
	selection.clear();
	expect(selection.is({ kind: 'check', pr: 'p', id: 1 })).toBe(false);
});

test('ids differ across kinds with the same key', () => {
	expect(selectedId({ kind: 'run', key: 'k' })).not.toBe(selectedId({ kind: 'event', key: 'k' }));
	expect(selectedId({ kind: 'review-wait', key: 'k' })).not.toBe(
		selectedId({ kind: 'event', key: 'k' })
	);
});
