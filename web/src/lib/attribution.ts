import type { Event, Story } from './model';
import { eventKey } from './keys';

function account(e: Event): string | undefined {
	return e.actor.login && `${e.source}/${e.instance}/${e.actor.login}`;
}

/**
 * Why each event's attribution to its account is uncertain, by event key;
 * an event nothing in the story makes uncertain has no entry.
 *
 * The rule, from explicit evidence only: a person's account with no known
 * executor is uncertain when the same account, on the same source, acted
 * for someone else elsewhere in the story (an agent posting through the
 * owner's GitHub user). The event may have been automation too; the story
 * cannot tell.
 */
export function uncertainAttributions(story: Story): Map<string, string> {
	const executors = new Map<string, Set<string>>();
	for (const e of story.events) {
		const acct = account(e);
		if (acct && e.executor && e.executor.name !== e.actor.login) {
			executors.set(acct, (executors.get(acct) ?? new Set()).add(e.executor.name));
		}
	}
	const uncertain = new Map<string, string>();
	for (const e of story.events) {
		const names = executors.get(account(e) ?? '');
		if (e.actor.kind === 'person' && !e.executor && names) {
			uncertain.set(
				eventKey(e),
				`${e.actor.login} also acted for ${[...names].sort().join(', ')} in this story`
			);
		}
	}
	return uncertain;
}

/** Why the event's attribution is uncertain, or undefined; see uncertainAttributions. */
export function attributionUncertain(story: Story, e: Event): string | undefined {
	return uncertainAttributions(story).get(eventKey(e));
}
