import type { Event, Story } from './model';

/**
 * Why the event's attribution to its account is uncertain, or undefined
 * when nothing in the story says so.
 *
 * The rule, from explicit evidence only: a person's account with no known
 * executor is uncertain when the same account, on the same source, acted
 * for someone else elsewhere in the story (an agent posting through the
 * owner's GitHub user). The event may have been automation too; the story
 * cannot tell.
 */
export function attributionUncertain(story: Story, e: Event): string | undefined {
	if (e.actor.kind !== 'person' || e.executor || !e.actor.login) {
		return undefined;
	}
	const executors = new Set<string>();
	for (const other of story.events) {
		if (
			other.source === e.source &&
			other.instance === e.instance &&
			other.actor.login === e.actor.login &&
			other.executor &&
			other.executor.name !== e.actor.login
		) {
			executors.add(other.executor.name);
		}
	}
	if (executors.size === 0) {
		return undefined;
	}
	return `${e.actor.login} also acted for ${[...executors].sort().join(', ')} in this story`;
}
