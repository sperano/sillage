// The client of sillage's read API. It makes one request per call and never
// retries: a source or server outage must show as an error, not as a slow
// page. A caller's AbortSignal cancels the request (a navigation away); its
// abort rejects with the signal's reason, which isAbort recognizes.

import { type Decoder, decode, DecodeError, array, object, text, timestamp } from './check';
import { coverageList, parseStory, root as rootDecoder } from './decodeModel';
import type { Coverage, Root, Story } from './model';
import { rootSegments, sameRoot } from './roots';
import { MS_PER_SECOND } from './format';

/** Where the read API is served, on the frontend's own origin. */
export const API_BASE = '/api/v1';

/** How long a request may take before it fails with a TimeoutError. */
export const REQUEST_TIMEOUT_MS = 15_000;

/**
 * The longest search the entry page sends. The API bounds a search's
 * length too; this keeps the box from offering what it would refuse.
 */
export const MAX_SEARCH_LENGTH = 200;

const HTTP_NOT_FOUND = 404;

/** A stored root a search matched. */
export interface RootSummary {
	root: Root;
	title?: string;
	/** The latest time of the root's records; absent when none has one. */
	lastActivity?: string;
}

const rootSummaries: Decoder<RootSummary[]> = array(
	object({ root: rootDecoder }, { title: text, lastActivity: timestamp })
);

/** The problem body the API answers an error with (RFC 9457). */
export interface Problem {
	title?: string;
	detail?: string;
}

/** A request to the read API failed. request names it: `GET /api/v1/coverage`. */
export class ApiError extends Error {
	override readonly name: string = 'ApiError';

	constructor(
		readonly request: string,
		message: string,
		options?: ErrorOptions
	) {
		super(`${request}: ${message}`, options);
	}
}

/** The server could not be reached: fetch itself failed. */
export class NetworkError extends ApiError {
	override readonly name = 'NetworkError';
}

/** The server did not answer within timeoutMs. */
export class TimeoutError extends ApiError {
	override readonly name = 'TimeoutError';

	constructor(
		request: string,
		readonly timeoutMs: number,
		options?: ErrorOptions
	) {
		super(request, `no answer within ${timeoutMs / MS_PER_SECOND} s`, options);
	}
}

/** The server answered with an error status. */
export class HttpError extends ApiError {
	override readonly name: string = 'HttpError';

	constructor(
		request: string,
		readonly status: number,
		readonly problem: Problem
	) {
		super(request, `${status} ${problem.title ?? ''}`.trim());
	}
}

/** The server has nothing at that address: no such story, or a link it cannot resolve. */
export class NotFoundError extends HttpError {
	override readonly name = 'NotFoundError';
}

/** The server answered, but not with what the API promises. */
export class InvalidResponseError extends ApiError {
	override readonly name = 'InvalidResponseError';
}

/** Whether err is the rejection of a request its caller aborted. */
export function isAbort(err: unknown): boolean {
	return err instanceof DOMException && err.name === 'AbortError';
}

export interface Api {
	/** The story of root. */
	story(root: Root, signal?: AbortSignal): Promise<Story>;
	/** The stored roots whose title or key contains query, most recently active first. */
	search(query: string, signal?: AbortSignal): Promise<RootSummary[]>;
	/** The root a link (a task, run, pull request or page) belongs to. */
	resolve(url: string, signal?: AbortSignal): Promise<Root>;
	/** How far each source instance has been read. */
	coverage(signal?: AbortSignal): Promise<Coverage[]>;
}

export interface ApiOptions {
	fetch?: typeof fetch;
	timeoutMs?: number;
}

/** The API's path for root's story. */
export function storyPath(root: Root): string {
	return `${API_BASE}/stories${rootSegments(root)}`;
}

export function createApi(options: ApiOptions = {}): Api {
	const fetchFn = options.fetch ?? globalThis.fetch.bind(globalThis);
	const timeoutMs = options.timeoutMs ?? REQUEST_TIMEOUT_MS;
	const get = <T>(path: string, parse: (body: unknown) => T, signal?: AbortSignal) =>
		request(fetchFn, timeoutMs, path, parse, signal);
	const query = (name: string, value: string) => `?${new URLSearchParams({ [name]: value })}`;
	return {
		story: async (root, signal) => {
			const path = storyPath(root);
			const story = await get(path, parseStory, signal);
			if (!sameRoot(story.root, root)) {
				throw new InvalidResponseError(`GET ${path}`, 'the story is of another root');
			}
			return story;
		},
		search: (q, signal) =>
			get(`${API_BASE}/search${query('q', q)}`, (b) => decode(rootSummaries, b), signal),
		resolve: (url, signal) =>
			get(`${API_BASE}/resolve${query('url', url)}`, (b) => decode(rootDecoder, b), signal),
		coverage: (signal) => get(`${API_BASE}/coverage`, (b) => decode(coverageList, b), signal)
	};
}

async function request<T>(
	fetchFn: typeof fetch,
	timeoutMs: number,
	path: string,
	parse: (body: unknown) => T,
	callerSignal?: AbortSignal
): Promise<T> {
	const name = `GET ${path}`;
	const timeout = AbortSignal.timeout(timeoutMs);
	const signal = callerSignal ? AbortSignal.any([callerSignal, timeout]) : timeout;
	try {
		const response = await fetchFn(path, { signal, headers: { Accept: 'application/json' } });
		if (!response.ok) throw await httpError(name, response);
		const body: unknown = await response.json().catch((err: unknown) => {
			if (signal.aborted) throw err;
			throw new InvalidResponseError(name, 'the body is not JSON', { cause: err });
		});
		return parse(body);
	} catch (err) {
		throw classify(name, err, timeoutMs, timeout, callerSignal);
	}
}

// classify turns what fetch, reading the body or parsing threw into an
// ApiError, except a caller's abort, which stays as it is.
function classify(
	name: string,
	err: unknown,
	timeoutMs: number,
	timeout: AbortSignal,
	callerSignal?: AbortSignal
): unknown {
	if (err instanceof ApiError) return err;
	if (callerSignal?.aborted) return callerSignal.reason;
	if (timeout.aborted) {
		return new TimeoutError(name, timeoutMs, { cause: err });
	}
	if (err instanceof DecodeError) {
		return new InvalidResponseError(name, `unexpected body: ${err.message}`, { cause: err });
	}
	return new NetworkError(name, 'the server could not be reached', { cause: err });
}

async function httpError(name: string, response: Response): Promise<HttpError> {
	const problem = await readProblem(response);
	if (!problem.title && response.statusText) problem.title = response.statusText;
	const ErrorClass = response.status === HTTP_NOT_FOUND ? NotFoundError : HttpError;
	return new ErrorClass(name, response.status, problem);
}

// readProblem reads the title and detail of an error's body, if it has a
// problem body; whatever else it holds is ignored.
async function readProblem(response: Response): Promise<Problem> {
	const body: unknown = await response.json().catch(() => undefined);
	if (typeof body !== 'object' || body === null) return {};
	const { title, detail } = body as Record<string, unknown>;
	const problem: Problem = {};
	if (typeof title === 'string' && title !== '') problem.title = title;
	if (typeof detail === 'string' && detail !== '') problem.detail = detail;
	return problem;
}
