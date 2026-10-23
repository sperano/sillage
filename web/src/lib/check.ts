// Decoders: functions that check an unknown JSON value against a shape and
// return it typed, or throw a DecodeError naming where it went wrong.

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
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/**
 * An RFC 3339 timestamp, kept as the string the source sent: converting it
 * to a Date would drop its offset and any precision below a millisecond.
 */
export const timestamp: Decoder<string> = (value, path) => {
	const s = anyString(value, path);
	if (!RFC3339.test(s) || Number.isNaN(Date.parse(s))) {
		fail(path, `expected an RFC 3339 time, got ${JSON.stringify(s)}`);
	}
	return s;
};

/** One of values. */
export function oneOf<const T extends string>(values: readonly T[]): Decoder<T> {
	return (value, path) => {
		const s = anyString(value, path);
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
	return value as Record<string, unknown>;
}

/** An object whose every key is a non-empty string and every value item decodes. */
export function record<T>(item: Decoder<T>): Decoder<Record<string, T>> {
	return (value, path) => {
		const obj = plainObject(value, path);
		const out: Record<string, T> = {};
		for (const [k, v] of Object.entries(obj)) {
			if (k === '') fail(path, 'an empty key');
			out[k] = item(v, `${path}.${k}`);
		}
		return out;
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
