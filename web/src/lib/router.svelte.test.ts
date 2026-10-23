import { afterEach, describe, expect, test } from 'vitest';
import { memoryHistory } from './history';
import { Router } from './router.svelte';

let cleanup: (() => void)[] = [];

afterEach(() => {
	cleanup.forEach((c) => c());
	cleanup = [];
});

// click clicks a link with attrs, which router handles; the test page
// itself never navigates.
function click(router: Router, attrs: Record<string, string>, init: MouseEventInit = {}): void {
	const anchor = document.createElement('a');
	for (const [k, v] of Object.entries(attrs)) anchor.setAttribute(k, v);
	document.body.append(anchor);
	const onClick = (event: MouseEvent) => {
		router.handleClick(event);
		event.preventDefault();
	};
	document.addEventListener('click', onClick);
	cleanup.push(() => {
		document.removeEventListener('click', onClick);
		anchor.remove();
	});
	anchor.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, ...init }));
}

describe('handleClick', () => {
	test('follows a link to an app route in place', () => {
		const history = memoryHistory('/');
		const router = new Router(history);
		click(router, { href: '/task/vikunja.example.com/7' });
		expect(router.route).toEqual({
			name: 'task',
			key: { instance: 'vikunja.example.com', id: 7 }
		});
		expect(history.entries()).toEqual(['/', '/task/vikunja.example.com/7']);
	});

	test.each<[string, Record<string, string>, MouseEventInit]>([
		['a modified click', { href: '/task/vikunja.example.com/7' }, { ctrlKey: true }],
		['a middle click', { href: '/task/vikunja.example.com/7' }, { button: 1 }],
		['another origin', { href: 'https://vikunja.example.com/tasks/7' }, {}],
		['a new tab', { href: '/task/vikunja.example.com/7', target: '_blank' }, {}],
		['a download', { href: '/task/vikunja.example.com/7', download: '' }, {}],
		['the API', { href: '/api/v1/coverage' }, {}],
		['a fragment of the current page', { href: '#sources' }, {}]
	])('leaves %s to the browser', (_, attrs, init) => {
		const history = memoryHistory('/');
		const router = new Router(history);
		click(router, attrs, init);
		expect(history.entries()).toEqual(['/']);
	});
});

test('start follows back and forward until stopped', () => {
	const history = memoryHistory('/');
	const router = new Router(history);
	const stop = router.start();
	router.navigate('/open?url=x');
	expect(router.route).toEqual({ name: 'open', url: 'x' });
	history.back();
	expect(router.route).toEqual({ name: 'home', query: '' });
	stop();
});

test('the same URL again keeps the route, and counts no navigation', () => {
	const router = new Router(memoryHistory('/task/vikunja.example.com/1'));
	const route = router.route;
	router.start()();
	router.navigate('/task/vikunja.example.com/1', { replace: true });
	expect(router.route).toBe(route);
	expect(router.navigations).toBe(0);
	router.navigate('/');
	expect(router.navigations).toBe(1);
});

test('navigate with replace drops the current entry', () => {
	const history = memoryHistory('/open?url=x');
	const router = new Router(history);
	router.navigate('/task/vikunja.example.com/1', { replace: true });
	expect(history.entries()).toEqual(['/task/vikunja.example.com/1']);
});
