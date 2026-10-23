import { expect, test } from 'vitest';
import { Selection } from './selection.svelte';

test('one ref at a time, compared by kind and id', () => {
	const s = new Selection();
	expect(s.current).toBeUndefined();
	s.select({ kind: 'run', id: 'cluster-a/agents/uid/x' });
	expect(s.isSelected({ kind: 'run', id: 'cluster-a/agents/uid/x' })).toBe(true);
	expect(s.isSelected({ kind: 'event', id: 'cluster-a/agents/uid/x' })).toBe(false);
	s.select({ kind: 'event', id: 'vikunja/vikunja.example.com/tasks/1' });
	expect(s.isSelected({ kind: 'run', id: 'cluster-a/agents/uid/x' })).toBe(false);
	s.select(undefined);
	expect(s.current).toBeUndefined();
});
