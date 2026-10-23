import { expect, test, vi } from 'vitest';
import { activation } from './keys';

type Phase = 'keydown' | 'keyup';

function press(phase: Phase, key: string, repeat = false) {
	const activate = vi.fn();
	const preventDefault = vi.fn();
	const e = { key, repeat, preventDefault } as unknown as KeyboardEvent;
	activation(activate)[phase === 'keydown' ? 'onkeydown' : 'onkeyup'](e);
	return { activated: activate.mock.calls.length, prevented: preventDefault.mock.calls.length > 0 };
}

test('Enter activates on keydown, and keeps the page from scrolling', () => {
	expect(press('keydown', 'Enter')).toEqual({ activated: 1, prevented: true });
	expect(press('keyup', 'Enter')).toEqual({ activated: 0, prevented: false });
});

test('Space activates on keyup, and keeps the page from scrolling on both', () => {
	expect(press('keydown', ' ')).toEqual({ activated: 0, prevented: true });
	expect(press('keyup', ' ')).toEqual({ activated: 1, prevented: true });
});

test('a held key activates once', () => {
	expect(press('keydown', 'Enter', true)).toEqual({ activated: 0, prevented: true });
	expect(press('keydown', ' ', true)).toEqual({ activated: 0, prevented: true });
});

test('other keys do nothing', () => {
	expect(press('keydown', 'Tab')).toEqual({ activated: 0, prevented: false });
	expect(press('keyup', 'Tab')).toEqual({ activated: 0, prevented: false });
});
