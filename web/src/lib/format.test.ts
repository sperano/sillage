import { expect, test } from 'vitest';
import { count, formatTime } from './format';

test.each([
	['2026-03-03T20:00:00Z', '2026-03-03 20:00:00 UTC'],
	['2026-03-03T16:00:00-04:00', '2026-03-03 20:00:00 UTC'],
	['2026-03-03T20:00:00.987654321Z', '2026-03-03 20:00:00 UTC']
])('%s reads %s', (time, want) => {
	expect(formatTime(time)).toBe(want);
});

test('count agrees the noun with the number', () => {
	expect(count(0, 'run')).toBe('0 runs');
	expect(count(1, 'run')).toBe('1 run');
	expect(count(2, 'pull request')).toBe('2 pull requests');
	expect(count(2, 'person', 'people')).toBe('2 people');
});
