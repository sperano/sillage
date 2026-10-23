import { describe, expect, test } from 'vitest';
import {
	DecodeError,
	array,
	decode,
	integer,
	object,
	oneOf,
	record,
	refine,
	fail,
	text,
	timestamp,
	anyString
} from './check';

function problem(run: () => unknown): DecodeError {
	try {
		run();
	} catch (err) {
		if (err instanceof DecodeError) return err;
		throw err;
	}
	throw new Error('decoded without an error');
}

describe('object', () => {
	const point = object({ x: integer }, { label: text });

	test('decodes required and optional fields', () => {
		expect(decode(point, { x: 1 })).toEqual({ x: 1 });
		expect(decode(point, { x: 1, label: 'a' })).toEqual({ x: 1, label: 'a' });
	});

	test.each<[string, unknown, string, string]>([
		['a missing field', {}, '$', 'missing field x'],
		['an unexpected field', { x: 1, y: 2 }, '$', 'unexpected field y'],
		['a null optional field', { x: 1, label: null }, '$.label', 'expected a string, got null'],
		['an empty optional string', { x: 1, label: '' }, '$.label', 'expected a non-empty string'],
		['not an object', [1], '$', 'expected an object, got an array'],
		['null', null, '$', 'expected an object, got null']
	])('rejects %s', (_, value, path, want) => {
		const err = problem(() => decode(point, value));
		expect(err.path).toBe(path);
		expect(err.problem).toBe(want);
	});

	test('ignores inherited properties', () => {
		const inherited = Object.create({ y: 2 }) as Record<string, unknown>;
		inherited.x = 1;
		expect(decode(point, inherited)).toEqual({ x: 1 });
	});
});

test('array paths name the element', () => {
	expect(problem(() => decode(array(integer), [1, 'two'])).path).toBe('$[1]');
});

test('record paths name the key, and a value may be empty', () => {
	expect(decode(record(anyString), { a: '' })).toEqual({ a: '' });
	expect(problem(() => decode(record(anyString), { a: 1 })).path).toBe('$.a');
});

test('oneOf rejects an unknown value and lists the known ones', () => {
	expect(decode(oneOf(['a', 'b']), 'a')).toBe('a');
	expect(problem(() => decode(oneOf(['a', 'b']), 'c')).problem).toBe(
		'unknown value "c"; expected one of a, b'
	);
});

test.each([1.5, Number.MAX_SAFE_INTEGER + 1, '1', NaN])('integer rejects %s', (value) => {
	expect(() => decode(integer, value)).toThrow(DecodeError);
});

test.each(['2026-03-03T20:00:00Z', '2026-03-03T20:00:00.5+02:00'])(
	'timestamp keeps %s',
	(value) => {
		expect(decode(timestamp, value)).toBe(value);
	}
);

test.each(['2026-03-03', '2026-03-03T20:00:00', '2026-13-03T20:00:00Z', 'yesterday'])(
	'timestamp rejects %s',
	(value) => {
		expect(() => decode(timestamp, value)).toThrow(DecodeError);
	}
);

test('refine reports its own problem at the path', () => {
	const even = refine(integer, (n, path) => {
		if (n % 2 !== 0) fail(path, 'odd');
	});
	expect(decode(array(even), [2])).toEqual([2]);
	expect(problem(() => decode(array(even), [3])).message).toBe('$[0]: odd');
});
