import { expect, test, vi } from 'vitest';
import { activation } from './keys';

type Phase = 'keydown' | 'keyup';

/** One mark's handlers, and how often they activated it. */
function mark() {
	const activate = vi.fn();
	const handlers = activation(activate);
	return {
		press(phase: Phase, key: string, repeat = false) {
			const preventDefault = vi.fn();
			const e = { key, repeat, preventDefault } as unknown as KeyboardEvent;
			handlers[phase === 'keydown' ? 'onkeydown' : 'onkeyup'](e);
			return preventDefault.mock.calls.length > 0;
		},
		blur: handlers.onblur,
		activations: () => activate.mock.calls.length
	};
}

test('Enter activates on keydown, and keeps the page from scrolling', () => {
	const m = mark();
	expect(m.press('keydown', 'Enter')).toBe(true);
	expect(m.activations()).toBe(1);
	expect(m.press('keyup', 'Enter')).toBe(false);
	expect(m.activations()).toBe(1);
});

test('Space activates on keyup, and keeps the page from scrolling on both', () => {
	const m = mark();
	expect(m.press('keydown', ' ')).toBe(true);
	expect(m.activations()).toBe(0);
	expect(m.press('keyup', ' ')).toBe(true);
	expect(m.activations()).toBe(1);
});

test('a held key activates once', () => {
	const m = mark();
	m.press('keydown', 'Enter');
	m.press('keydown', 'Enter', true);
	m.press('keydown', ' ');
	m.press('keydown', ' ', true);
	m.press('keyup', ' ');
	expect(m.activations()).toBe(2);
});

test('a Space pressed elsewhere, or before the mark lost focus, does not activate', () => {
	const m = mark();
	m.press('keyup', ' ');
	m.press('keydown', ' ');
	m.blur();
	m.press('keyup', ' ');
	expect(m.activations()).toBe(0);
});

test('other keys do nothing', () => {
	const m = mark();
	expect(m.press('keydown', 'Tab')).toBe(false);
	expect(m.press('keyup', 'Tab')).toBe(false);
	expect(m.activations()).toBe(0);
});
