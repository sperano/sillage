import { describe, expect, test } from 'vitest';
import {
	DecodeError,
	anyString,
	array,
	bool,
	compareTimes,
	decode,
	fail,
	integer,
	nonEmpty,
	nonNegativeInteger,
	object,
	oneOf,
	positiveInteger,
	record,
	refine,
	text,
	timestamp,
	unique
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
	const proto = decode(record(anyString), JSON.parse('{"__proto__": "x"}'));
	expect(Object.keys(proto)).toEqual(['__proto__']);
	expect(problem(() => decode(record(anyString), { a: 1 })).path).toBe('$.a');
});

test('record rejects an empty key', () => {
	const err = problem(() => decode(record(anyString), { '': 'x' }));
	expect(err.path).toBe('$');
	expect(err.problem).toBe('an empty key');
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

test('bool takes true and false, and nothing else', () => {
	expect(decode(bool, true)).toBe(true);
	expect(decode(bool, false)).toBe(false);
	expect(problem(() => decode(bool, 'true')).problem).toBe('expected a boolean, got a string');
	expect(problem(() => decode(bool, 0)).problem).toBe('expected a boolean, got a number');
});

test('positiveInteger starts at one, nonNegativeInteger at zero', () => {
	expect(decode(positiveInteger, 1)).toBe(1);
	expect(problem(() => decode(positiveInteger, 0)).problem).toBe(
		'expected a positive integer, got 0'
	);
	expect(decode(nonNegativeInteger, 0)).toBe(0);
	expect(problem(() => decode(nonNegativeInteger, -1)).problem).toBe(
		'expected a non-negative integer, got -1'
	);
	expect(() => decode(positiveInteger, 1.5)).toThrow('expected a safe integer');
	expect(() => decode(nonNegativeInteger, 0.5)).toThrow('expected a safe integer');
});

test('nonEmpty rejects an empty list', () => {
	expect(decode(nonEmpty(array(integer)), [1])).toEqual([1]);
	const err = problem(() => decode(nonEmpty(array(integer)), []));
	expect(err.path).toBe('$');
	expect(err.problem).toBe('at least one is required');
});

test('unique rejects a second element with a key seen, at that element', () => {
	const byId = unique(array(object({ id: integer }, { label: text })), (p) => String(p.id));
	expect(decode(byId, [{ id: 1 }, { id: 2 }])).toEqual([{ id: 1 }, { id: 2 }]);
	const err = problem(() => decode(byId, [{ id: 1 }, { id: 2 }, { id: 1, label: 'again' }]));
	expect(err.path).toBe('$[2]');
	expect(err.problem).toBe('duplicate record 1');
});

test.each([
	'2026-03-03T20:00:00Z',
	'2026-03-03T20:00:00.5+02:00',
	'2028-02-29T00:00:00Z',
	'2000-02-29T00:00:00Z',
	'0000-02-29T00:00:00Z',
	'0004-02-29T00:00:00Z'
])('timestamp keeps %s', (value) => {
	expect(decode(timestamp, value)).toBe(value);
});

test.each([
	'2026-03-03',
	'2026-03-03T20:00:00',
	'2026-13-03T20:00:00Z',
	'2026-02-29T00:00:00Z',
	'1900-02-29T00:00:00Z',
	'0001-02-29T00:00:00Z',
	'2026-04-31T00:00:00Z',
	'2026-01-01T24:00:00Z',
	'2026-01-01T23:60:00Z',
	'2026-12-31T23:59:60Z',
	'2026-01-01T00:00:00+24:00',
	'yesterday'
])('timestamp rejects %s', (value) => {
	expect(() => decode(timestamp, value)).toThrow(DecodeError);
});

describe('compareTimes', () => {
	test.each<[string, string, number]>([
		['2026-03-03T20:00:00Z', '2026-03-03T20:00:01Z', -1],
		['2026-03-03T20:00:00Z', '2026-03-03T22:00:00+02:00', 0],
		['2026-03-03T20:00:00Z', '2026-03-03T16:00:00-04:00', 0],
		['2026-03-03T20:00:00.5Z', '2026-03-03T20:00:00.500000000Z', 0],
		['2026-03-03T20:00:00.000000001Z', '2026-03-03T20:00:00.000000002Z', -1],
		['2026-03-03T20:00:00.0001Z', '2026-03-03T20:00:00Z', 1],
		['2026-03-03T20:00:00.9Z', '2026-03-03T20:00:01Z', -1],
		['0050-01-01T00:00:00Z', '1950-01-01T00:00:00Z', -1]
	])('%s against %s', (a, b, want) => {
		expect(Math.sign(compareTimes(a, b))).toBe(want);
		expect(Math.sign(compareTimes(b, a))).toBe(-want || 0);
	});

	test('throws on a string that is not a time', () => {
		expect(() => compareTimes('2026-03-03T20:00:00Z', 'yesterday')).toThrow(RangeError);
	});
});

test('refine reports its own problem at the path', () => {
	const even = refine(integer, (n, path) => {
		if (n % 2 !== 0) fail(path, 'odd');
	});
	expect(decode(array(even), [2])).toEqual([2]);
	expect(problem(() => decode(array(even), [3])).message).toBe('$[0]: odd');
});
