// The client of sillage's read API. It makes one request per call and never
// retries: a source or server outage must show as an error, not as a slow
// page. A caller's AbortSignal cancels the request (a navigation away); its
// abort rejects with the signal's reason, which isAbort recognizes when the
// caller aborted without one of its own.

import { DecodeError, decode } from './check';
import {
	type Freshness,
	type Resolution,
	type RootSummary,
	configResponse,
	freshnessResponse,
	resolution,
	searchResponse
} from './decodeApi';
import { parseStory } from './decodeModel';
import type { LinkConfig } from './links';
import type { Root, Story } from './model';
import { rootSegments, sameRoot } from './roots';
import { MS_PER_SECOND } from './format';
import { parseOverviewPage } from './overview/decode';
import type { OverviewPage } from './overview/model';
import { overviewApiParams, type OverviewQuery } from './overview/query';

/** Where the read API is served, on the frontend's own origin. */
export const API_BASE = '/api/v1';

/** How long a request may take before it fails with a TimeoutError. */
export const REQUEST_TIMEOUT_MS = 15_000;

/**
 * The longest search the entry page sends: the box takes no more, and a
 * longer one from a link is not sent. The API bounds a search's length too;
 * this keeps the page from offering what it would refuse.
 */
export const MAX_SEARCH_LENGTH = 200;

const HTTP_NOT_FOUND = 404;

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
	/**
	 * The task roots a link (a task, run or pull request) belongs to, most
	 * recently active first, and what it was read as.
	 */
	resolve(url: string, signal?: AbortSignal): Promise<Resolution>;
	/**
	 * How recently each source instance was read, across the stored roots,
	 * and which sources are not read.
	 */
	freshness(signal?: AbortSignal): Promise<Freshness>;
	/**
	 * One page of the overview: the tasks active in the query's window that
	 * match its filters, after the cursor when one is given.
	 */
	overview(query: OverviewQuery, after?: string, signal?: AbortSignal): Promise<OverviewPage>;
	/** What the frontend links with: the Kelos console of each cluster. */
	config(signal?: AbortSignal): Promise<LinkConfig>;
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
		search: async (q, signal) =>
			(await get(`${API_BASE}/search${query('q', q)}`, (b) => decode(searchResponse, b), signal))
				.roots,
		resolve: (url, signal) =>
			get(`${API_BASE}/resolve${query('url', url)}`, (b) => decode(resolution, b), signal),
		freshness: (signal) => get(`${API_BASE}/coverage`, (b) => decode(freshnessResponse, b), signal),
		overview: (q, after, signal) => {
			const params = overviewApiParams(q, after).toString();
			const path = params ? `${API_BASE}/overview?${params}` : `${API_BASE}/overview`;
			return get(path, parseOverviewPage, signal);
		},
		config: (signal) => get(`${API_BASE}/config`, (b) => decode(configResponse, b), signal)
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
			// Anything else (an abort, a connection dropped mid-body) is classified below.
			if (err instanceof SyntaxError) {
				throw new InvalidResponseError(name, 'the body is not JSON', { cause: err });
			}
			throw err;
		});
		return parse(body);
	} catch (err) {
		throw classify(name, err, timeoutMs, timeout, callerSignal);
	}
}

// classify turns what fetch, reading the body or parsing threw into an
// ApiError, except a caller's abort, which stays as it is. An abort or a
// timeout wins over the error it caused, even one already classified: an
// error response whose body read was cut short is the abort, not the status.
function classify(
	name: string,
	err: unknown,
	timeoutMs: number,
	timeout: AbortSignal,
	callerSignal?: AbortSignal
): unknown {
	if (callerSignal?.aborted) return callerSignal.reason;
	if (timeout.aborted) {
		return new TimeoutError(name, timeoutMs, { cause: err });
	}
	if (err instanceof ApiError) return err;
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
	const problem: Problem = {};
	if ('title' in body && typeof body.title === 'string' && body.title !== '') {
		problem.title = body.title;
	}
	if ('detail' in body && typeof body.detail === 'string' && body.detail !== '') {
		problem.detail = body.detail;
	}
	return problem;
}
