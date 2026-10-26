import type { CheckRun, Event, PRKey, PullRequest, Run, Story, TaskKey } from './model';
import { runKeyString } from './keys';

// Links to the record a source holds. Vikunja and GitHub instances are web
// hosts: the collector keeps a source's host alone,
// without its scheme or port, so they are linked over https on the
// default port, and a source served otherwise gets wrong links. A Kelos
// instance is a cluster name, so its console's URL comes from
// configuration.

export interface LinkConfig {
	/** The Kelos console's base URL per cluster name. */
	kelosConsoles?: Readonly<Record<string, string>>;
}

export interface SourceLink {
	href: string;
	label: string;
}

/** An event id's number after prefix ("comments/5501" → "5501"), if it has that form. */
function idAfter(id: string, prefix: string): string | undefined {
	if (!id.startsWith(prefix)) {
		return undefined;
	}
	const rest = id.slice(prefix.length);
	return /^[0-9]+$/.test(rest) ? rest : undefined;
}

/** The schemes a link may have: anything else, javascript: above all, is never one. */
const LINK_PROTOCOLS: ReadonlySet<string> = new Set(['https:', 'http:']);

/**
 * The URL under base of the path segments, each encoded (a segment's own
 * slashes, as in owner/name, separate segments), with an
 * optional query and fragment; undefined when base is not an http or
 * https URL.
 */
function under(
	base: string,
	segments: readonly (string | number)[],
	extra: { query?: Readonly<Record<string, string>>; hash?: string } = {}
): string | undefined {
	let url: URL;
	try {
		url = new URL(base);
	} catch {
		return undefined;
	}
	if (!LINK_PROTOCOLS.has(url.protocol)) {
		return undefined;
	}
	const parts = segments.flatMap((s) => String(s).split('/')).map(encodeURIComponent);
	url.pathname = [url.pathname.replace(/\/+$/, ''), ...parts].join('/');
	url.search = new URLSearchParams(extra.query).toString();
	url.hash = extra.hash ?? '';
	return url.href;
}

/** The https origin of a source instance, which is its host; undefined if it is not one. */
function origin(host: string): string | undefined {
	try {
		const url = new URL(`https://${host}`);
		return url.host === host.toLowerCase() && url.pathname === '/' ? url.origin : undefined;
	} catch {
		return undefined;
	}
}

function onHost(
	host: string,
	segments: readonly (string | number)[],
	extra?: { query?: Readonly<Record<string, string>>; hash?: string }
): string | undefined {
	const base = origin(host);
	return base && under(base, segments, extra);
}

export function taskURL(k: TaskKey, hash?: string): string | undefined {
	return onHost(k.instance, ['tasks', k.id], { hash });
}

export function prURL(k: PRKey, hash?: string): string | undefined {
	return onHost(k.instance, [k.repository, 'pull', k.number], { hash });
}

export function checkURL(pr: PullRequest, check: CheckRun): string | undefined {
	return onHost(pr.key.instance, [pr.key.repository, 'runs', check.id]);
}

/** A link, or none when its URL could not be built. */
function link(href: string | undefined, label: string): SourceLink | undefined {
	return href === undefined ? undefined : { href, label };
}

function vikunjaLink(e: Event): SourceLink | undefined {
	const task = e.related.task;
	if (!task) {
		return undefined;
	}
	const comment = idAfter(e.id, 'comments/');
	return comment
		? link(taskURL(task, `comment-${comment}`), `Vikunja comment ${comment}`)
		: link(taskURL(task), `Vikunja task ${task.id}`);
}

/** GitHub's anchor for each kind of record the GitHub source reads. */
const githubAnchors: [prefix: string, anchor: string, label: string][] = [
	['issues/comments/', 'issuecomment-', 'GitHub comment'],
	['pulls/comments/', 'discussion_r', 'GitHub review comment'],
	['reviews/', 'pullrequestreview-', 'GitHub review'],
	['issues/events/', 'event-', 'GitHub event']
];

function githubLink(e: Event): SourceLink | undefined {
	const pr = e.related.pr;
	if (!pr) {
		return undefined;
	}
	for (const [prefix, anchor, label] of githubAnchors) {
		const id = idAfter(e.id, prefix);
		if (id) {
			return link(prURL(pr, `${anchor}${id}`), `${label} ${id}`);
		}
	}
	return link(prURL(pr), `${pr.repository}#${pr.number}`);
}

/**
 * The Kelos console's view of the run's Task logs. The console finds a Task
 * by name, which a retry reuses, and a deleted Task is gone from it.
 */
export function runLink(run: Run, config: LinkConfig): SourceLink | undefined {
	const base = config.kelosConsoles?.[run.key.cluster];
	if (!base || !run.taskName) {
		return undefined;
	}
	return link(
		under(base, ['api', 'resources', 'tasks', run.key.namespace, run.taskName, 'logs']),
		`Kelos console: Task ${run.taskName}`
	);
}

/** The source record behind the event, when its keys locate one. */
export function eventLink(story: Story, e: Event, config: LinkConfig): SourceLink | undefined {
	switch (e.source) {
		case 'vikunja':
			return vikunjaLink(e);
		case 'github':
			return githubLink(e);
		case 'kelos': {
			const key = e.related.run && runKeyString(e.related.run);
			const run = key && story.runs.find((r) => runKeyString(r.key) === key);
			return run ? runLink(run, config) : undefined;
		}
	}
}
