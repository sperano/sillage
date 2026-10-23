import type { CheckRun, Event, PageKey, PRKey, PullRequest, Run, Story, TaskKey } from './model';
import { pageKeyString, runKeyString } from './keys';

// Links to the record a source holds. Vikunja, GitHub and Wiki.js
// instances are web hosts; a Kelos instance is a cluster name, so its
// console's URL comes from configuration.

const SCHEME = 'https://';

export interface LinkConfig {
	/** The Kelos console's base URL per cluster name, without a trailing slash. */
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

export function taskURL(k: TaskKey): string {
	return `${SCHEME}${k.instance}/tasks/${k.id}`;
}

export function prURL(k: PRKey): string {
	return `${SCHEME}${k.instance}/${k.repository}/pull/${k.number}`;
}

export function checkURL(pr: PullRequest, check: CheckRun): string {
	return `${SCHEME}${pr.key.instance}/${pr.key.repository}/runs/${check.id}`;
}

/**
 * The page's current path, from the story's last move of it, else its
 * creation; undefined when no event of the story names it.
 */
export function pagePath(story: Story, page: PageKey): string | undefined {
	const key = pageKeyString(page);
	let path: string | undefined;
	for (const e of story.events) {
		if (e.source !== 'wiki' || !e.related.page || pageKeyString(e.related.page) !== key) {
			continue;
		}
		if (e.kind === 'page.created') {
			path ??= e.details?.path;
		} else if (e.kind === 'page.moved' && e.details?.to) {
			path = e.details.to;
		}
	}
	return path;
}

/** The page's URL: by path when known, else Wiki.js's redirect by id. */
export function pageURL(story: Story, page: PageKey): string {
	const path = pagePath(story, page);
	return path
		? `${SCHEME}${page.instance}/${page.locale}/${path}`
		: `${SCHEME}${page.instance}/i/${page.id}`;
}

function vikunjaLink(e: Event): SourceLink | undefined {
	const task = e.related.task;
	if (!task) {
		return undefined;
	}
	const comment = idAfter(e.id, 'comments/');
	return comment
		? { href: `${taskURL(task)}#comment-${comment}`, label: `Vikunja comment ${comment}` }
		: { href: taskURL(task), label: `Vikunja task ${task.id}` };
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
			return { href: `${prURL(pr)}#${anchor}${id}`, label: `${label} ${id}` };
		}
	}
	return { href: prURL(pr), label: `${pr.repository}#${pr.number}` };
}

function wikiLink(story: Story, e: Event): SourceLink | undefined {
	const page = e.related.page;
	if (!page) {
		return undefined;
	}
	const comment = idAfter(e.id, 'comments/');
	if (comment) {
		return {
			href: `${pageURL(story, page)}#comment-post-id-${comment}`,
			label: `wiki comment ${comment}`
		};
	}
	const version = idAfter(e.id, `pages/${page.id}/versions/`);
	const path = pagePath(story, page);
	if (version && path) {
		return {
			href: `${SCHEME}${page.instance}/s/${page.locale}/${path}?v=${version}`,
			label: `wiki page version ${version}`
		};
	}
	const live = e.kind === 'page.edited' && e.details?.version === 'live';
	return { href: pageURL(story, page), label: live ? 'wiki page, live version' : 'wiki page' };
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
	return {
		href: `${base}/api/resources/tasks/${run.key.namespace}/${run.taskName}/logs`,
		label: `Kelos console: Task ${run.taskName}`
	};
}

/** The source record behind the event, when its keys locate one. */
export function eventLink(story: Story, e: Event, config: LinkConfig): SourceLink | undefined {
	switch (e.source) {
		case 'vikunja':
			return vikunjaLink(e);
		case 'github':
			return githubLink(e);
		case 'wiki':
			return wikiLink(story, e);
		case 'kelos': {
			const key = e.related.run && runKeyString(e.related.run);
			const run = key && story.runs.find((r) => runKeyString(r.key) === key);
			return run ? runLink(run, config) : undefined;
		}
	}
}
