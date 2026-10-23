import { describe, expect, test, vi } from 'vitest';
import {
	API_BASE,
	HttpError,
	InvalidResponseError,
	NetworkError,
	NotFoundError,
	TimeoutError,
	createApi,
	isAbort
} from './api';
import type { Root } from './model';
import { json, problem } from '../testing/fakeServer';
import { goldenStory } from '../testing/goldenStories';

const TASK: Root = { task: { instance: 'vikunja.example.com', id: 102 } };
const PAGE: Root = { page: { instance: 'wiki.example.com', id: 57, locale: 'en' } };
const SHORT_TIMEOUT_MS = 20;

// answering returns a fetch that answers every request with respond's
// response and records the paths asked for.
function answering(respond: () => Response | Promise<Response>) {
	return vi.fn<typeof fetch>(async () => respond());
}

// hanging returns a fetch that never answers until its signal aborts.
function hanging() {
	return vi.fn<typeof fetch>(
		(_input, init) =>
			new Promise((_resolve, reject) => {
				init?.signal?.addEventListener('abort', () => reject(init.signal?.reason));
			})
	);
}

function requested(fetchFn: ReturnType<typeof vi.fn<typeof fetch>>): string[] {
	return fetchFn.mock.calls.map(([input]) => String(input));
}

describe('requests', () => {
	test('story asks for the task or page path and decodes the story', async () => {
		const fetchFn = answering(() => json(goldenStory('implementation-merged')));
		const story = await createApi({ fetch: fetchFn }).story(TASK);
		expect(story.root).toEqual(TASK);
		expect(requested(fetchFn)).toEqual([`${API_BASE}/stories/task/vikunja.example.com/102`]);

		const pageFetch = answering(() => json(goldenStory('wiki-rooted-request')));
		await createApi({ fetch: pageFetch }).story(PAGE);
		expect(requested(pageFetch)).toEqual([`${API_BASE}/stories/page/wiki.example.com/57/en`]);
	});

	test('a story of another root is an invalid response', async () => {
		const fetchFn = answering(() => json(goldenStory('implementation-merged')));
		const other: Root = { task: { instance: 'vikunja.example.com', id: 999 } };
		await expect(createApi({ fetch: fetchFn }).story(other)).rejects.toThrow(InvalidResponseError);
	});

	test('search and resolve encode their parameter', async () => {
		const searchFetch = answering(() =>
			json([{ root: TASK, title: 'Widgets', lastActivity: '2026-03-03T19:30:00Z' }, { root: PAGE }])
		);
		const hits = await createApi({ fetch: searchFetch }).search('a&b c');
		expect(hits).toEqual([
			{ root: TASK, title: 'Widgets', lastActivity: '2026-03-03T19:30:00Z' },
			{ root: PAGE }
		]);
		expect(requested(searchFetch)).toEqual([`${API_BASE}/search?q=a%26b+c`]);

		const resolveFetch = answering(() => json(TASK));
		const link = 'https://github.example.com/example-org/widgets/pull/7';
		expect(await createApi({ fetch: resolveFetch }).resolve(link)).toEqual(TASK);
		expect(requested(resolveFetch)).toEqual([
			`${API_BASE}/resolve?url=${encodeURIComponent(link).replaceAll('%20', '+')}`
		]);
	});

	test('coverage decodes every source instance', async () => {
		const coverage = [
			{ source: 'kelos', instance: 'cluster-a', lastSuccessfulRead: '2026-03-03T20:00:00Z' },
			{ source: 'wiki', instance: 'wiki.example.com' }
		];
		const fetchFn = answering(() => json(coverage));
		expect(await createApi({ fetch: fetchFn }).coverage()).toEqual(coverage);
		expect(requested(fetchFn)).toEqual([`${API_BASE}/coverage`]);
	});
});

describe('errors', () => {
	test('404 is a NotFoundError with the problem', async () => {
		const fetchFn = answering(() => problem(404, 'Not Found', 'no story for that task'));
		const err: unknown = await createApi({ fetch: fetchFn })
			.story(TASK)
			.catch((e: unknown) => e);
		expect(err).toBeInstanceOf(NotFoundError);
		expect(err).toMatchObject({
			status: 404,
			problem: { title: 'Not Found', detail: 'no story for that task' }
		});
	});

	test('another status is an HttpError, with or without a problem body', async () => {
		const withBody = answering(() => problem(503, 'Service Unavailable', 'store unreachable'));
		await expect(createApi({ fetch: withBody }).coverage()).rejects.toMatchObject({
			name: 'HttpError',
			status: 503,
			problem: { title: 'Service Unavailable', detail: 'store unreachable' }
		});

		const bare = answering(() => new Response('oops', { status: 500, statusText: 'Internal' }));
		const err: unknown = await createApi({ fetch: bare })
			.coverage()
			.catch((e: unknown) => e);
		expect(err).toBeInstanceOf(HttpError);
		expect(err).not.toBeInstanceOf(NotFoundError);
		expect(err).toMatchObject({ status: 500, problem: { title: 'Internal' } });
	});

	test('a body that is not JSON, or not the model, is an invalid response', async () => {
		const html = answering(() => new Response('<!doctype html>', { status: 200 }));
		await expect(createApi({ fetch: html }).coverage()).rejects.toThrow(InvalidResponseError);

		const wrong = answering(() => json([{ source: 'jira', instance: 'x' }]));
		await expect(createApi({ fetch: wrong }).coverage()).rejects.toThrow(/unknown value "jira"/);
	});

	test('a fetch that fails is a NetworkError, tried once', async () => {
		const fetchFn = vi.fn<typeof fetch>(async () => {
			throw new TypeError('Failed to fetch');
		});
		await expect(createApi({ fetch: fetchFn }).coverage()).rejects.toThrow(NetworkError);
		expect(fetchFn).toHaveBeenCalledOnce();
	});

	test('a server that does not answer is a TimeoutError, tried once', async () => {
		const fetchFn = hanging();
		const api = createApi({ fetch: fetchFn, timeoutMs: SHORT_TIMEOUT_MS });
		await expect(api.coverage()).rejects.toThrow(TimeoutError);
		expect(fetchFn).toHaveBeenCalledOnce();
	});

	test("the caller's abort rejects with its reason, not an ApiError", async () => {
		const controller = new AbortController();
		const pending = createApi({ fetch: hanging() }).story(TASK, controller.signal);
		controller.abort();
		const err: unknown = await pending.catch((e: unknown) => e);
		expect(isAbort(err)).toBe(true);
	});
});
