// A fake read API for component tests: a fetch that answers from handlers
// instead of a server, and records each request with its signal, so that a
// test can check what was asked for and what was aborted.

import { createApi, type Api } from '../lib/api';
import { MEMORY_ORIGIN } from '../lib/history';

/** Answers a request; HANG never answers until the request is aborted. */
export type Handler = (url: URL) => Response | typeof HANG;

export const HANG = Symbol('hang');

export interface Request {
	/** The path and query asked for. */
	path: string;
	signal: AbortSignal;
}

export interface FakeServer {
	api: Api;
	requests: Request[];
}

const HTTP_NOT_FOUND = 404;

/** A JSON response with body. */
export function json(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

/** An error response with a problem body. */
export function problem(status: number, title: string, detail?: string): Response {
	return new Response(JSON.stringify({ type: 'about:blank', title, status, detail }), {
		status,
		headers: { 'Content-Type': 'application/problem+json' }
	});
}

/** A fake server answering each path (without its query) with its handler, and 404 otherwise. */
export function fakeServer(handlers: Record<string, Handler>): FakeServer {
	const requests: Request[] = [];
	const fakeFetch: typeof fetch = (input, init) => {
		const url = new URL(String(input), MEMORY_ORIGIN);
		const signal = init?.signal ?? new AbortController().signal;
		requests.push({ path: url.pathname + url.search, signal });
		const handler = handlers[url.pathname];
		const answer = handler ? handler(url) : problem(HTTP_NOT_FOUND, 'Not Found');
		if (answer !== HANG) return Promise.resolve(answer);
		return new Promise((_resolve, reject) => {
			signal.addEventListener('abort', () => reject(signal.reason as Error));
		});
	};
	return { api: createApi({ fetch: fakeFetch }), requests };
}
