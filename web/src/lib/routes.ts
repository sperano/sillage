// The frontend's routes and their URLs. Routing uses the history API, not
// the hash: a link such as /open?url=... or /task/<instance>/<id> is a
// plain URL that the Vikunja start announcement, the portal or a pasted
// address can carry unchanged. For such a deep link to load the app, the Go
// server must answer every non-API path with index.html (AGENTS.md,
// "Routes").

import type { Root, TaskKey } from './model';
import { parseOverviewQuery, type OverviewQuery } from './overview/query';
import { rootSegments } from './roots';

export type Route =
	/** The entry page; query is the search, empty when none was made. */
	| { name: 'home'; query: string }
	| { name: 'task'; key: TaskKey }
	/** Resolve url to its root, then show that root's story. */
	| { name: 'open'; url: string }
	/** What was working on what: the tasks active in a window. */
	| { name: 'overview'; query: OverviewQuery }
	| { name: 'not-found'; path: string };

/** The search parameter of the entry page. */
export const QUERY_PARAM = 'q';
/** The link parameter of /open. */
export const URL_PARAM = 'url';

// A positive id as the API writes it: digits without a leading zero.
const ID = /^[1-9][0-9]*$/;

function parseID(segment: string): number | undefined {
	if (!ID.test(segment)) return undefined;
	const id = Number(segment);
	return Number.isSafeInteger(id) ? id : undefined;
}

// segments returns the decoded segments of path, which is not "/",
// ignoring one trailing slash; undefined when one is malformed or empty.
function segments(path: string): string[] | undefined {
	const trimmed = path.endsWith('/') ? path.slice(0, -1) : path;
	try {
		const parts = trimmed.split('/').slice(1).map(decodeURIComponent);
		return parts.includes('') ? undefined : parts;
	} catch {
		return undefined;
	}
}

function parseRoot(parts: readonly string[]): Route | undefined {
	const [kind, instance, rawID, ...rest] = parts;
	if (instance === undefined || rawID === undefined || rest.length > 0) return undefined;
	const id = parseID(rawID);
	if (id === undefined) return undefined;
	if (kind === 'task') return { name: 'task', key: { instance, id } };
	return undefined;
}

/** The route url shows. */
export function parseRoute(url: URL): Route {
	if (url.pathname === '/') {
		return { name: 'home', query: url.searchParams.get(QUERY_PARAM) ?? '' };
	}
	const parts = segments(url.pathname);
	const notFound: Route = { name: 'not-found', path: url.pathname };
	if (parts === undefined) return notFound;
	if (parts.length === 1 && parts[0] === 'open') {
		return { name: 'open', url: url.searchParams.get(URL_PARAM) ?? '' };
	}
	if (parts.length === 1 && parts[0] === 'overview') {
		return { name: 'overview', query: parseOverviewQuery(url.searchParams) };
	}
	return parseRoot(parts) ?? notFound;
}

/** The entry page's URL, with query as its search when there is one. */
export function homeHref(query = ''): string {
	return query === '' ? '/' : `/?${new URLSearchParams({ [QUERY_PARAM]: query })}`;
}

/** The URL that resolves link and shows its story. */
export function openHref(link: string): string {
	return `/open?${new URLSearchParams({ [URL_PARAM]: link })}`;
}

/** The URL of root's story. */
export function rootHref(root: Root): string {
	return rootSegments(root);
}
