import type { Event, PRKey, Ref, RunKey, TaskKey } from './model';

// The string forms of internal/model's keys, which correlation refs carry.
// They must stay byte for byte what the Go String methods return.

export function taskKeyString(k: TaskKey): string {
	return `${k.instance}/tasks/${k.id}`;
}

export function runKeyString(k: RunKey): string {
	return k.uid !== undefined
		? `${k.cluster}/${k.namespace}/uid/${k.uid}`
		: `${k.cluster}/${k.namespace}/pod/${k.pod}`;
}

export function prKeyString(k: PRKey): string {
	return `${k.instance}/${k.repository}/pull/${k.number}`;
}

export function eventKey(e: Event): string {
	return `${e.source}/${e.instance}/${e.id}`;
}

export function sameRef(a: Ref, b: Ref): boolean {
	return a.kind === b.kind && a.id === b.id;
}
