// Roots, the tasks a story is about: comparing and naming them.

import type { Root, TaskKey } from './model';

function sameTask(a: TaskKey, b: TaskKey): boolean {
	return a.instance === b.instance && a.id === b.id;
}

/** Whether a and b are the same task. */
export function sameRoot(a: Root, b: Root): boolean {
	return sameTask(a.task, b.task);
}

/** What a reader calls root: "task 102 on vikunja.example.com". */
export function rootLabel(root: Root): string {
	return `task ${root.task.id} on ${root.task.instance}`;
}

/** A root's short name, when its title is unknown: "task 201". */
export function rootName(root: Root): string {
	return `task ${root.task.id}`;
}

/**
 * root's key as URL path segments, "/task/<instance>/<id>": the shape of
 * both the API's story paths and the frontend's story routes.
 */
export function rootSegments(root: Root): string {
	const enc = encodeURIComponent;
	return `/task/${enc(root.task.instance)}/${root.task.id}`;
}
