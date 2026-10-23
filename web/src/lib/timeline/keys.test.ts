import { expect, test, vi } from 'vitest';
import { onActivate } from './keys';

function press(key: string) {
	const activate = vi.fn();
	const e = { key, preventDefault: vi.fn() } as unknown as KeyboardEvent;
	onActivate(activate)(e);
	return {
		activate,
		prevented: (e.preventDefault as ReturnType<typeof vi.fn>).mock.calls.length > 0
	};
}

test('Enter and Space activate, and keep the page from scrolling', () => {
	expect(press('Enter')).toMatchObject({ prevented: true });
	expect(press('Enter').activate).toHaveBeenCalledOnce();
	expect(press(' ').activate).toHaveBeenCalledOnce();
});

test('other keys do nothing', () => {
	const { activate, prevented } = press('Tab');
	expect(activate).not.toHaveBeenCalled();
	expect(prevented).toBe(false);
});
