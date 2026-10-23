import type { Source, Story } from './model';

export interface SourceInstance {
	source: Source;
	instance: string;
}

function key(s: SourceInstance): string {
	return `${s.source}/${s.instance}`;
}

/**
 * The source instances the story's records come from that have no
 * coverage record: how far they were read is unknown, so nothing in the
 * story says their records are complete.
 */
export function uncoveredSources(story: Story): SourceInstance[] {
	const seen = new Map<string, SourceInstance>();
	const add = (s: SourceInstance) => seen.set(key(s), s);
	for (const e of story.events) {
		add({ source: e.source, instance: e.instance });
	}
	for (const r of story.runs) {
		add({ source: 'kelos', instance: r.key.cluster });
	}
	for (const p of story.pullRequests) {
		add({ source: 'github', instance: p.key.instance });
	}
	const covered = new Set(story.coverage.map(key));
	return [...seen.values()]
		.filter((s) => !covered.has(key(s)))
		.sort((a, b) => key(a).localeCompare(key(b)));
}

const sourceNames: Record<Source, string> = {
	vikunja: 'Vikunja',
	kelos: 'Kelos',
	github: 'GitHub',
	wiki: 'Wiki.js'
};

export function sourceName(s: Source): string {
	return sourceNames[s] ?? s;
}
