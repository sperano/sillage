import { expect, test } from 'vitest';
import { named } from './named';

const names: Record<string, string> = { a: 'Alpha' };

test('a known value has its name; any other is itself', () => {
	expect(named<string, string>(names, 'a')).toBe('Alpha');
	expect(named<string, string>(names, 'b')).toBe('b');
	expect(named<string, string>(names, 'constructor')).toBe('constructor');
});
