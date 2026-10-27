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
import { DEFAULT_QUERY } from './overview/query';
import { overviewGoldenJSON } from './testing/overview';
import { goldenPaths, goldenResponse } from './testing/apiResponses';

const TASK: Root = { task: { instance: 'vikunja.example.com', id: 102 } };
const OTHER_TASK: Root = { task: { instance: 'vikunja.example.com', id: 103 } };
const SHORT_TIMEOUT_MS = 20;
const PULL_LINK = 'https://github.example.com/example-org/widgets/pull/7';

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
	test('story asks for the task path and decodes the story', async () => {
		const fetchFn = answering(() => json(goldenStory('implementation-merged')));
		const story = await createApi({ fetch: fetchFn }).story(TASK);
		expect(story.root).toEqual(TASK);
		expect(requested(fetchFn)).toEqual([`${API_BASE}/stories/task/vikunja.example.com/102`]);
	});

	test('a story of another root is an invalid response', async () => {
		const fetchFn = answering(() => json(goldenStory('implementation-merged')));
		const other: Root = { task: { instance: 'vikunja.example.com', id: 999 } };
		await expect(createApi({ fetch: fetchFn }).story(other)).rejects.toThrow(InvalidResponseError);
	});

	test('search and resolve encode their parameter', async () => {
		const searchFetch = answering(() =>
			json({
				query: 'a&b c',
				roots: [
					{ root: TASK, title: 'Widgets', lastActivity: '2026-03-03T19:30:00Z' },
					{ root: OTHER_TASK }
				]
			})
		);
		const hits = await createApi({ fetch: searchFetch }).search('a&b c');
		expect(hits).toEqual([
			{ root: TASK, title: 'Widgets', lastActivity: '2026-03-03T19:30:00Z' },
			{ root: OTHER_TASK }
		]);
		expect(requested(searchFetch)).toEqual([`${API_BASE}/search?q=a%26b+c`]);

		const resolution = {
			kind: 'github-pull-request',
			roots: [{ root: TASK }, { root: OTHER_TASK }]
		};
		const resolveFetch = answering(() => json(resolution));
		const link = 'https://github.example.com/example-org/widgets/pull/7';
		expect(await createApi({ fetch: resolveFetch }).resolve(link)).toEqual(resolution);
		expect(requested(resolveFetch)).toEqual([
			`${API_BASE}/resolve?${new URLSearchParams({ url: link })}`
		]);
	});

	test('a resolution without a root is an invalid response', async () => {
		const fetchFn = answering(() => json({ kind: 'vikunja-task', roots: [] }));
		await expect(createApi({ fetch: fetchFn }).resolve('https://x.example.com')).rejects.toThrow(
			InvalidResponseError
		);
	});

	test('freshness decodes every source instance', async () => {
		const sources = [
			{
				source: 'kelos',
				instance: 'cluster-a',
				roots: 3,
				neverRead: 1,
				newestRead: '2026-03-03T20:00:00Z',
				oldestRead: '2026-03-02T20:00:00Z'
			},
			{ source: 'github', instance: 'github.example.com', roots: 1, neverRead: 1 }
		];
		const fetchFn = answering(() => json({ sources }));
		expect(await createApi({ fetch: fetchFn }).freshness()).toEqual(sources);
		expect(requested(fetchFn)).toEqual([`${API_BASE}/coverage`]);
	});

	test('bare lists, not the envelopes the server answers, are invalid responses', async () => {
		const fetchFn = answering(() => json([]));
		const api = createApi({ fetch: fetchFn });
		await expect(api.search('widgets')).rejects.toThrow(InvalidResponseError);
		await expect(api.freshness()).rejects.toThrow(InvalidResponseError);
		await expect(createApi({ fetch: answering(() => json(TASK)) }).resolve('x')).rejects.toThrow(
			InvalidResponseError
		);
	});

	test('overview sends the filters and the cursor, and decodes the page', async () => {
		const fetchFn = answering(() => json(overviewGoldenJSON()));
		const api = createApi({ fetch: fetchFn });
		expect((await api.overview(DEFAULT_QUERY)).rows).toHaveLength(7);
		await api.overview({ ...DEFAULT_QUERY, project: 'widgets', failed: true }, 'next-page');
		expect(requested(fetchFn)).toEqual([
			`${API_BASE}/overview`,
			`${API_BASE}/overview?project=widgets&failed=true&after=next-page`
		]);
	});

	test('an overview page that is not one is an invalid response', async () => {
		const fetchFn = answering(() => json({ rows: [] }));
		await expect(createApi({ fetch: fetchFn }).overview(DEFAULT_QUERY)).rejects.toThrow(
			InvalidResponseError
		);
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
		await expect(createApi({ fetch: withBody }).freshness()).rejects.toMatchObject({
			name: 'HttpError',
			status: 503,
			problem: { title: 'Service Unavailable', detail: 'store unreachable' }
		});

		const bare = answering(() => new Response('oops', { status: 500, statusText: 'Internal' }));
		const err: unknown = await createApi({ fetch: bare })
			.freshness()
			.catch((e: unknown) => e);
		expect(err).toBeInstanceOf(HttpError);
		expect(err).not.toBeInstanceOf(NotFoundError);
		expect(err).toMatchObject({ status: 500, problem: { title: 'Internal' } });
	});

	test('a body that is not JSON, or not the model, is an invalid response', async () => {
		const html = answering(() => new Response('<!doctype html>', { status: 200 }));
		await expect(createApi({ fetch: html }).freshness()).rejects.toThrow(InvalidResponseError);

		const wrong = answering(() =>
			json({ sources: [{ source: 'jira', instance: 'x', roots: 1, neverRead: 1 }] })
		);
		await expect(createApi({ fetch: wrong }).freshness()).rejects.toThrow(/unknown value "jira"/);
	});

	test('a connection dropped while reading the body is a NetworkError', async () => {
		const response = json({ sources: [] });
		vi.spyOn(response, 'json').mockRejectedValue(new TypeError('network error'));
		const fetchFn = answering(() => response);
		await expect(createApi({ fetch: fetchFn }).freshness()).rejects.toThrow(NetworkError);
	});

	test('a fetch that fails is a NetworkError, tried once', async () => {
		const fetchFn = vi.fn<typeof fetch>(async () => {
			throw new TypeError('Failed to fetch');
		});
		await expect(createApi({ fetch: fetchFn }).freshness()).rejects.toThrow(NetworkError);
		expect(fetchFn).toHaveBeenCalledOnce();
	});

	test('a server that does not answer is a TimeoutError, tried once', async () => {
		const fetchFn = hanging();
		const api = createApi({ fetch: fetchFn, timeoutMs: SHORT_TIMEOUT_MS });
		await expect(api.freshness()).rejects.toThrow(TimeoutError);
		expect(fetchFn).toHaveBeenCalledOnce();
	});

	test('an abort while reading an error body rejects with the abort, not the status', async () => {
		const controller = new AbortController();
		const response = problem(503, 'Service Unavailable');
		vi.spyOn(response, 'json').mockImplementation(() => {
			controller.abort();
			return Promise.reject(controller.signal.reason as Error);
		});
		const err: unknown = await createApi({ fetch: answering(() => response) })
			.freshness(controller.signal)
			.catch((e: unknown) => e);
		expect(isAbort(err)).toBe(true);
	});

	test('a timeout while reading an error body is a TimeoutError', async () => {
		// An error status whose body never arrives until the request is aborted.
		const fetchFn = vi.fn<typeof fetch>(async (_input, init) => {
			const response = problem(503, 'Service Unavailable');
			vi.spyOn(response, 'json').mockImplementation(
				() =>
					new Promise((_resolve, reject) => {
						init?.signal?.addEventListener('abort', () => reject(init.signal?.reason));
					})
			);
			return response;
		});
		const api = createApi({ fetch: fetchFn, timeoutMs: SHORT_TIMEOUT_MS });
		await expect(api.freshness()).rejects.toThrow(TimeoutError);
	});

	test("the caller's abort rejects with its reason, not an ApiError", async () => {
		const controller = new AbortController();
		const pending = createApi({ fetch: hanging() }).story(TASK, controller.signal);
		controller.abort();
		const err: unknown = await pending.catch((e: unknown) => e);
		expect(isAbort(err)).toBe(true);
	});
});

// What the Go handlers answer, from their golden file: each request the
// client makes for it is the path the file has, and decodes.
describe('the Go responses', () => {
	const answeringGolden = () =>
		vi.fn<typeof fetch>(async (input) => json(goldenResponse(String(input))));

	test('the file has a body for each request below', () => {
		expect([...goldenPaths].sort()).toEqual(
			[
				`${API_BASE}/config`,
				`${API_BASE}/coverage`,
				`${API_BASE}/resolve?${new URLSearchParams({ url: PULL_LINK })}`,
				`${API_BASE}/search?q=nothing-matches`,
				`${API_BASE}/search?q=retry`
			].sort()
		);
	});

	test('searches decode', async () => {
		const api = createApi({ fetch: answeringGolden() });
		expect(await api.search('retry')).toEqual([
			{ root: { task: { instance: 'vikunja.example.com', id: 104 } }, title: 'failed-run-retry' }
		]);
		expect(await api.search('nothing-matches')).toEqual([]);
	});

	test('a resolved link decodes', async () => {
		const resolution = await createApi({ fetch: answeringGolden() }).resolve(PULL_LINK);
		expect(resolution).toEqual({
			kind: 'github-pull-request',
			roots: [
				{
					root: { task: { instance: 'vikunja.example.com', id: 102 } },
					title: 'implementation-merged'
				}
			]
		});
	});

	test('the freshness decodes, one record per source instance', async () => {
		const sources = await createApi({ fetch: answeringGolden() }).freshness();
		expect(sources.map((s) => `${s.source}/${s.instance}`)).toEqual([
			'github/github.example.com',
			'kelos/cluster-a',
			'vikunja/vikunja.example.com'
		]);
		expect(sources.every((s) => s.newestRead !== undefined && s.oldestRead !== undefined)).toBe(
			true
		);
	});

	test('the configuration decodes, the console keyed by cluster', async () => {
		expect(await createApi({ fetch: answeringGolden() }).config()).toEqual({
			kelosConsoles: { 'cluster-a': 'https://kelos.example.com' }
		});
	});
});
