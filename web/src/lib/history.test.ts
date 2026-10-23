import { expect, test, vi } from 'vitest';
import { memoryHistory } from './history';

test('push adds an entry after the current one, dropping those ahead', () => {
	const h = memoryHistory('/');
	h.push('/a');
	h.push('/b?x=1');
	h.back();
	h.push('/c');
	expect(h.entries()).toEqual(['/', '/a', '/c']);
	expect(h.location().pathname).toBe('/c');
});

test('replace swaps the current entry', () => {
	const h = memoryHistory('/open?url=x');
	h.replace('/task/vikunja.example.com/1');
	expect(h.entries()).toEqual(['/task/vikunja.example.com/1']);
});

test('back tells the listeners, until they stop listening', () => {
	const h = memoryHistory('/');
	h.push('/a');
	const onChange = vi.fn();
	const stop = h.listen(onChange);
	h.back();
	expect(onChange).toHaveBeenCalledOnce();
	expect(h.location().pathname).toBe('/');
	stop();
	h.back();
	expect(onChange).toHaveBeenCalledOnce();
});
