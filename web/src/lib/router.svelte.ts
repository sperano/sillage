// The router: the current route, as reactive state, over a History.

import { API_BASE } from './api';
import type { History } from './history';
import { type Route, parseRoute } from './routes';

const PRIMARY_BUTTON = 0;

export class Router {
	/** The route the current URL shows. */
	route: Route = $state.raw({ name: 'home', query: '' });
	/** How many times the route changed since the first. */
	navigations = $state(0);

	readonly #history: History;
	// The URL route was parsed from: a sync to the same URL keeps the route,
	// so that what depends on it (a story's request) does not start again.
	#href = '';

	constructor(history: History) {
		this.#history = history;
		this.#sync();
	}

	/** Follows the history's back and forward moves until the returned function is called. */
	start(): () => void {
		this.#sync();
		return this.#history.listen(() => this.#sync());
	}

	/** Shows href; with replace, in place of the current entry, so that back skips it. */
	navigate(href: string, { replace = false } = {}): void {
		if (replace) this.#history.replace(href);
		else this.#history.push(href);
		this.#sync();
	}

	/**
	 * Handles a click on a link to one of the app's own routes, without
	 * reloading the page. A click the browser should handle (a modified or
	 * middle click, another origin, a download, the API, a fragment of the
	 * current page) is left to it.
	 */
	handleClick(event: MouseEvent): void {
		const href = this.#ownHref(event);
		if (href === undefined) return;
		event.preventDefault();
		this.navigate(href);
	}

	#ownHref(event: MouseEvent): string | undefined {
		if (event.defaultPrevented || event.button !== PRIMARY_BUTTON) return undefined;
		if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return undefined;
		const anchor = event.target instanceof Element ? event.target.closest('a') : null;
		if (!anchor || anchor.hasAttribute('download')) return undefined;
		if (anchor.target !== '' && anchor.target !== '_self') return undefined;
		const raw = anchor.getAttribute('href');
		if (raw === null) return undefined;
		const here = this.#history.location();
		// A value computed and dropped here, not state: nothing reacts to it.
		// eslint-disable-next-line svelte/prefer-svelte-reactivity
		const url = new URL(raw, here);
		if (url.origin !== here.origin) return undefined;
		if (url.pathname === API_BASE || url.pathname.startsWith(`${API_BASE}/`)) return undefined;
		const own = url.pathname + url.search;
		if (url.hash !== '' && own === here.pathname + here.search) return undefined;
		return own + url.hash;
	}

	#sync(): void {
		const url = this.#history.location();
		if (url.href === this.#href) return;
		if (this.#href !== '') this.navigations++;
		this.#href = url.href;
		this.route = parseRoute(url);
	}
}
