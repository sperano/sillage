// Roots, the tasks and pages a story is about: comparing and naming them.

import type { PageKey, Root, TaskKey } from './model';

function sameTask(a: TaskKey, b: TaskKey): boolean {
	return a.instance === b.instance && a.id === b.id;
}

function samePage(a: PageKey, b: PageKey): boolean {
	return a.instance === b.instance && a.id === b.id && a.locale === b.locale;
}

/** Whether a and b are the same task or the same page. */
export function sameRoot(a: Root, b: Root): boolean {
	if (a.task && b.task) return sameTask(a.task, b.task);
	if (a.page && b.page) return samePage(a.page, b.page);
	return false;
}

/** What a reader calls root: "task 102 on vikunja.example.com". */
export function rootLabel(root: Root): string {
	if (root.task) return `task ${root.task.id} on ${root.task.instance}`;
	return `page ${root.page.id} (${root.page.locale}) on ${root.page.instance}`;
}

/** A root's short name, when its title is unknown: "task 201", "page 57 (en)". */
export function rootName(root: Root): string {
	return root.task ? `task ${root.task.id}` : `page ${root.page.id} (${root.page.locale})`;
}

/**
 * root's key as URL path segments, "/task/<instance>/<id>" or
 * "/page/<instance>/<id>/<locale>": the shape of both the API's story
 * paths and the frontend's story routes.
 */
export function rootSegments(root: Root): string {
	const enc = encodeURIComponent;
	if (root.task) return `/task/${enc(root.task.instance)}/${root.task.id}`;
	const { instance, id, locale } = root.page;
	return `/page/${enc(instance)}/${id}/${enc(locale)}`;
}
