// Decoders: functions that check an unknown JSON value against a shape and
// return it typed, or throw a DecodeError naming where it went wrong.

import { MS_PER_SECOND } from './format';

/** A value did not have the expected shape. */
export class DecodeError extends Error {
	override readonly name = 'DecodeError';

	constructor(
		/** Where in the value it failed, as a path from its root: `$.runs[0].key`. */
		readonly path: string,
		/** What was wrong there. */
		readonly problem: string
	) {
		super(`${path}: ${problem}`);
	}
}

/** Checks value, found at path, and returns it typed. */
export type Decoder<T> = (value: unknown, path: string) => T;

/** The root of a decoded value's path. */
export const ROOT_PATH = '$';

/** Decodes value from the root, throwing a DecodeError on a mismatch. */
export function decode<T>(decoder: Decoder<T>, value: unknown): T {
	return decoder(value, ROOT_PATH);
}

function describe(value: unknown): string {
	if (value === null) return 'null';
	if (Array.isArray(value)) return 'an array';
	return `a ${typeof value}`;
}

/** Fails at path: for a check a decoder cannot express. */
export function fail(path: string, problem: string): never {
	throw new DecodeError(path, problem);
}

/** A string, empty or not. */
export const anyString: Decoder<string> = (value, path) => {
	if (typeof value !== 'string') fail(path, `expected a string, got ${describe(value)}`);
	return value;
};

/** A non-empty string. */
export const text: Decoder<string> = (value, path) => {
	const s = anyString(value, path);
	if (s === '') fail(path, 'expected a non-empty string');
	return s;
};

/** A boolean. */
export const bool: Decoder<boolean> = (value, path) => {
	if (typeof value !== 'boolean') fail(path, `expected a boolean, got ${describe(value)}`);
	return value;
};

/**
 * An integer JavaScript represents exactly. A Go int64 beyond 2^53 would
 * lose precision in JSON.parse, so it fails rather than decoding wrong.
 */
export const integer: Decoder<number> = (value, path) => {
	if (typeof value !== 'number' || !Number.isSafeInteger(value)) {
		fail(path, `expected a safe integer, got ${JSON.stringify(value)}`);
	}
	return value;
};

/** An integer above zero. */
export const positiveInteger: Decoder<number> = (value, path) => {
	const n = integer(value, path);
	if (n <= 0) fail(path, `expected a positive integer, got ${n}`);
	return n;
};

/** An integer not below zero. */
export const nonNegativeInteger: Decoder<number> = (value, path) => {
	const n = integer(value, path);
	if (n < 0) fail(path, `expected a non-negative integer, got ${n}`);
	return n;
};

// What Go's time.Time marshals to: RFC 3339 with optional fractional
// seconds, and Z or a numeric offset.
const RFC3339 =
	/^(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})T(?<hour>\d{2}):(?<minute>\d{2}):(?<second>\d{2})(?:\.(?<fraction>\d+))?(?:Z|(?<offsetSign>[+-])(?<offsetHour>\d{2}):(?<offsetMinute>\d{2}))$/;

const MAX_MONTH = 12;
const MAX_HOUR = 23;
const MAX_MINUTE = 59;
const MAX_SECOND = 59;
const SECONDS_PER_MINUTE = 60;
const SECONDS_PER_HOUR = 3600;

// utcMs is the Unix time in milliseconds of a UTC date and time; unlike
// Date.UTC, it keeps a year from 0 to 99 as it is, not as 1900 to 1999.
function utcMs(year: number, month: number, day: number, hour = 0, minute = 0, second = 0) {
	const d = new Date(0);
	d.setUTCFullYear(year, month - 1, day);
	d.setUTCHours(hour, minute, second);
	return d.getTime();
}

// The number of days in month (1 to 12) of year: day 0 of the next month is
// its last day.
function daysIn(year: number, month: number): number {
	return new Date(utcMs(year, month + 1, 0)).getUTCDate();
}

// An instant at full precision: whole Unix seconds, and the digits of the
// fraction of a second as written.
interface Instant {
	seconds: number;
	fraction: string;
}

// parseTime returns the instant s names when s is an RFC 3339 time whose
// every field is in range, as Go's time.Parse checks it: no February 30, no
// hour 24, no leap second, which Date.parse would accept or roll over.
function parseTime(s: string): Instant | undefined {
	const g = RFC3339.exec(s)?.groups;
	if (g === undefined) return undefined;
	const year = Number(g.year);
	const month = Number(g.month);
	const day = Number(g.day);
	const hour = Number(g.hour);
	const minute = Number(g.minute);
	const second = Number(g.second);
	// Z has no offset: zero.
	const offsetHour = Number(g.offsetHour ?? 0);
	const offsetMinute = Number(g.offsetMinute ?? 0);
	const inRange =
		month >= 1 &&
		month <= MAX_MONTH &&
		day >= 1 &&
		day <= daysIn(year, month) &&
		hour <= MAX_HOUR &&
		minute <= MAX_MINUTE &&
		second <= MAX_SECOND &&
		offsetHour <= MAX_HOUR &&
		offsetMinute <= MAX_MINUTE;
	if (!inRange) return undefined;
	const offset = offsetHour * SECONDS_PER_HOUR + offsetMinute * SECONDS_PER_MINUTE;
	const local = utcMs(year, month, day, hour, minute, second) / MS_PER_SECOND;
	return {
		seconds: g.offsetSign === '-' ? local + offset : local - offset,
		fraction: g.fraction ?? ''
	};
}

/**
 * An RFC 3339 timestamp, kept as the string the source sent: converting it
 * to a Date would drop its offset and any precision below a millisecond.
 */
export const timestamp: Decoder<string> = (value, path) => {
	const s = anyString(value, path);
	if (parseTime(s) === undefined) fail(path, `expected an RFC 3339 time, got ${JSON.stringify(s)}`);
	return s;
};

/**
 * Compares two timestamps at full precision, below the millisecond
 * Date.parse keeps: negative when a is before b, zero when they are the
 * same instant, positive when a is after b.
 */
export function compareTimes(a: string, b: string): number {
	const x = parseTime(a);
	const y = parseTime(b);
	if (x === undefined || y === undefined) {
		throw new RangeError(`not an RFC 3339 time: ${JSON.stringify(x ? b : a)}`);
	}
	if (x.seconds !== y.seconds) return x.seconds - y.seconds;
	// Padded to one length, fractions compare as their digits do: .5 is .500.
	const width = Math.max(x.fraction.length, y.fraction.length);
	const fx = x.fraction.padEnd(width, '0');
	const fy = y.fraction.padEnd(width, '0');
	if (fx === fy) return 0;
	return fx < fy ? -1 : 1;
}

/** One of values. */
export function oneOf<const T extends string>(values: readonly T[]): Decoder<T> {
	return (value, path) => {
		const s = anyString(value, path);
		// Widened to search it for any string; the includes check narrows s to T.
		if (!(values as readonly string[]).includes(s)) {
			fail(path, `unknown value ${JSON.stringify(s)}; expected one of ${values.join(', ')}`);
		}
		return s as T;
	};
}

/** An array whose every element item decodes. */
export function array<T>(item: Decoder<T>): Decoder<T[]> {
	return (value, path) => {
		if (!Array.isArray(value)) fail(path, `expected an array, got ${describe(value)}`);
		return value.map((v, i) => item(v, `${path}[${i}]`));
	};
}

function plainObject(value: unknown, path: string): Record<string, unknown> {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) {
		fail(path, `expected an object, got ${describe(value)}`);
	}
	// A non-null, non-array object: JSON's objects are string-keyed.
	return value as Record<string, unknown>;
}

/** An object whose every key is a non-empty string and every value item decodes. */
export function record<T>(item: Decoder<T>): Decoder<Record<string, T>> {
	return (value, path) => {
		const entries = Object.entries(plainObject(value, path)).map(([k, v]): [string, T] => {
			if (k === '') fail(path, 'an empty key');
			return [k, item(v, `${path}.${k}`)];
		});
		// fromEntries defines each key, so that one named __proto__ stays a key.
		return Object.fromEntries(entries);
	};
}

type Fields = Record<string, Decoder<unknown>>;
type Decoded<F extends Fields> = { [K in keyof F]: ReturnType<F[K]> };

/**
 * An object with the required and optional fields given, and no other: a
 * field the model gained, or lost, fails here rather than being dropped or
 * read as missing. An optional field is absent when Go omitted it
 * (omitempty); a null fails, since Go writes no null for these.
 */
export function object<R extends Fields, O extends Fields = Record<never, never>>(
	required: R,
	optional?: O
): Decoder<Decoded<R> & Partial<Decoded<O>>> {
	const opt: Fields = optional ?? {};
	return (value, path) => {
		const obj = plainObject(value, path);
		const out: Record<string, unknown> = {};
		for (const [k, decoder] of Object.entries(required)) {
			if (!Object.hasOwn(obj, k)) fail(path, `missing field ${k}`);
			out[k] = decoder(obj[k], `${path}.${k}`);
		}
		for (const [k, decoder] of Object.entries(opt)) {
			if (Object.hasOwn(obj, k)) out[k] = decoder(obj[k], `${path}.${k}`);
		}
		for (const k of Object.keys(obj)) {
			if (!Object.hasOwn(required, k) && !Object.hasOwn(opt, k)) {
				fail(path, `unexpected field ${k}`);
			}
		}
		// Each required key was set from its decoder, each optional one when present.
		return out as Decoded<R> & Partial<Decoded<O>>;
	};
}

/** decoder, then a check of the decoded value that throws via fail. */
export function refine<T>(
	decoder: Decoder<T>,
	check: (value: T, path: string) => void
): Decoder<T> {
	return (value, path) => {
		const decoded = decoder(value, path);
		check(decoded, path);
		return decoded;
	};
}

/** A list decoder's list, which must not be empty. */
export function nonEmpty<T>(decoder: Decoder<T[]>): Decoder<T[]> {
	return refine(decoder, (list, path) => {
		if (list.length === 0) fail(path, 'at least one is required');
	});
}

/**
 * A list decoder's list, without two elements of the same key: the Go
 * model never builds a story holding one record twice.
 */
export function unique<T>(decoder: Decoder<T[]>, key: (item: T) => string): Decoder<T[]> {
	return refine(decoder, (list, path) => {
		const seen = new Set<string>();
		list.forEach((item, i) => {
			const k = key(item);
			if (seen.has(k)) fail(`${path}[${i}]`, `duplicate record ${k}`);
			seen.add(k);
		});
	});
}
