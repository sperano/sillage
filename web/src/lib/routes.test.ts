import { describe, expect, test } from 'vitest';
import { DEFAULT_QUERY } from './overview/query';
import { type Route, homeHref, openHref, parseRoute, rootHref } from './routes';

const ORIGIN = 'http://sillage.example.com';

function route(href: string): Route {
	return parseRoute(new URL(href, ORIGIN));
}

describe('parseRoute', () => {
	test.each<[string, Route]>([
		['/', { name: 'home', query: '' }],
		['/?q=widgets%20task', { name: 'home', query: 'widgets task' }],
		[
			'/task/vikunja.example.com/102',
			{ name: 'task', key: { instance: 'vikunja.example.com', id: 102 } }
		],
		[
			'/task/vikunja.example.com/102/',
			{ name: 'task', key: { instance: 'vikunja.example.com', id: 102 } }
		],
		[
			'/task/vikunja.example.com%3A8443/1',
			{ name: 'task', key: { instance: 'vikunja.example.com:8443', id: 1 } }
		],
		[
			'/open?url=https%3A%2F%2Fvikunja.example.com%2Ftasks%2F102',
			{ name: 'open', url: 'https://vikunja.example.com/tasks/102' }
		],
		['/open', { name: 'open', url: '' }],
		['/overview', { name: 'overview', query: DEFAULT_QUERY }],
		[
			'/overview?executor=agent-cedar&review=true&group=executor',
			{
				name: 'overview',
				query: { ...DEFAULT_QUERY, executor: 'agent-cedar', review: true, group: 'executor' }
			}
		]
	])('%s', (href, want) => {
		expect(route(href)).toEqual(want);
	});

	test.each([
		'/task',
		'/task/vikunja.example.com',
		'/task/vikunja.example.com/0',
		'/task/vikunja.example.com/012',
		'/task/vikunja.example.com/-1',
		'/task/vikunja.example.com/1e3',
		'/task/vikunja.example.com/9007199254740993',
		'/task/vikunja.example.com/102/en',
		'/task//102',
		'/task/%E0%A4%A/102',
		'/page/wiki.example.com/41',
		'/page/wiki.example.com/41/en/extra',
		'/open/more',
		'/elsewhere'
	])('%s is not a route', (href) => {
		expect(route(href)).toEqual({ name: 'not-found', path: new URL(href, ORIGIN).pathname });
	});
});

describe('hrefs', () => {
	test('round-trip through parseRoute', () => {
		const task = { instance: 'vikunja.example.com:8443', id: 7 };
		expect(route(rootHref({ task }))).toEqual({ name: 'task', key: task });
		expect(route(homeHref('a&b=c'))).toEqual({ name: 'home', query: 'a&b=c' });
		expect(route(openHref('https://x.example.com/?a=1#b'))).toEqual({
			name: 'open',
			url: 'https://x.example.com/?a=1#b'
		});
	});

	test('the entry page without a search has no query', () => {
		expect(homeHref()).toBe('/');
		expect(homeHref('')).toBe('/');
	});
});
