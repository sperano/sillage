import type {
	Account,
	CheckConclusion,
	CheckRun,
	Event,
	EventKind,
	Outcome,
	PullRequest,
	Ref,
	Run,
	RunRole,
	Story
} from './model';
import { eventKey, pageKeyString, prKeyString, runKeyString, taskKeyString } from './keys';
import { pagePath } from './links';
import { named } from './named';
import type { TimelineItem } from './timeline';

// Plain words for records: what the event list and the detail panel say.

/** How many characters of a commit's SHA identify it in a label. */
const SHORT_SHA = 7;

type Details = Readonly<Record<string, string>>;

const eventLabels: Record<EventKind, (d: Details) => string> = {
	'task.created': () => 'Task created',
	'task.bucket_observed': (d) => `Seen in bucket ${d.bucket ?? '(unnamed)'}`,
	'task.label_observed': (d) => `Seen with label ${d.label ?? '(unnamed)'}`,
	'task.assignee_observed': (d) => `Seen assigned to ${d.assignee ?? '(unnamed)'}`,
	'task.done': () => 'Task marked done',
	'page.created': () => 'Page created',
	'page.edited': (d) =>
		d.version === 'live'
			? 'Page edited (live version)'
			: d.version
				? `Page edited (version ${d.version})`
				: 'Page edited',
	'page.moved': (d) => (d.to ? `Page moved to ${d.to}` : 'Page moved'),
	'comment.created': () => 'Comment',
	'request.created': (d) =>
		`Request ${d.command ?? ''}${d.agent ? ` for ${d.agent}` : ''}`.trimEnd(),
	'run.announced': (d) => (d.taskName ? `Run announced: ${d.taskName}` : 'Run announced'),
	'pr.opened': () => 'Pull request opened',
	'pr.review_requested': (d) => `Review requested from ${d.reviewer ?? d.team ?? '(unnamed)'}`,
	'pr.review_request_removed': (d) =>
		`Review request for ${d.reviewer ?? d.team ?? '(unnamed)'} removed`,
	'pr.review_submitted': (d) => (d.state ? `Review submitted: ${d.state}` : 'Review submitted'),
	'pr.merged': () => 'Pull request merged',
	'pr.closed': () => 'Pull request closed without merge',
	'pr.reopened': () => 'Pull request reopened'
};

export function eventLabel(e: Event): string {
	const label = named(eventLabels, e.kind);
	return typeof label === 'function' ? label(e.details ?? {}) : label;
}

const roleLabels: Record<RunRole, string> = {
	implementation: 'implementation',
	discussion: 'discussion',
	'task-follow-up': 'task follow-up',
	'pr-follow-up': 'pull request follow-up',
	'wiki-follow-up': 'wiki follow-up',
	'conflict-fix': 'merge conflict fix',
	automation: 'automation',
	unknown: 'role unknown'
};

export function roleLabel(role: RunRole): string {
	return named(roleLabels, role);
}

const outcomeLabels: Record<Outcome, string> = {
	succeeded: 'succeeded',
	failed: 'failed',
	waiting: 'waiting (queued at the last read)',
	running: 'running at the last read',
	unknown: 'outcome unknown'
};

export function outcomeLabel(outcome: Outcome): string {
	return named(outcomeLabels, outcome);
}

const conclusionLabels: Record<CheckConclusion, string> = {
	pending: 'pending',
	success: 'succeeded',
	failure: 'failed',
	cancelled: 'cancelled',
	timed_out: 'timed out',
	neutral: 'neutral',
	skipped: 'skipped',
	action_required: 'action required',
	stale: 'stale'
};

export function conclusionLabel(conclusion: CheckConclusion): string {
	return named(conclusionLabels, conclusion);
}

export function runName(run: Run): string {
	return run.taskName ?? run.pod ?? runKeyString(run.key);
}

export function runLabel(run: Run): string {
	return `Run ${runName(run)} (${roleLabel(run.role)}): ${outcomeLabel(run.outcome)}`;
}

export function checkLabel(pr: PullRequest, c: CheckRun): string {
	return `Check ${c.name} on ${c.sha.slice(0, SHORT_SHA)} (${pr.key.repository}#${pr.key.number}): ${conclusionLabel(c.conclusion)}`;
}

export function itemLabel(item: TimelineItem): string {
	switch (item.kind) {
		case 'event':
			return eventLabel(item.event);
		case 'run':
			return runLabel(item.run);
		case 'check':
			return checkLabel(item.pr, item.check);
	}
}

export function accountLabel(a: Account): string {
	return a.login ? `${a.login} (${a.kind})` : 'unknown (seen by polling)';
}

/** What a correlation's end is, in words. */
export function refLabel(story: Story, ref: Ref): string {
	switch (ref.kind) {
		case 'event': {
			const e = story.events.find((x) => eventKey(x) === ref.id);
			return e ? eventLabel(e) : `event ${ref.id}`;
		}
		case 'run': {
			const r = story.runs.find((x) => runKeyString(x.key) === ref.id);
			return r ? `Run ${runName(r)}` : `run ${ref.id}`;
		}
		case 'pr': {
			const p = story.pullRequests.find((x) => prKeyString(x.key) === ref.id);
			return p ? `Pull request ${p.key.repository}#${p.key.number}` : `pull request ${ref.id}`;
		}
		case 'task':
			return story.root.task && taskKeyString(story.root.task) === ref.id
				? `Task ${story.root.task.id} (this story)`
				: `task ${ref.id}`;
		case 'page': {
			if (story.root.page && pageKeyString(story.root.page) === ref.id) {
				return `Page ${story.root.page.id} (this story)`;
			}
			const page = story.events.find(
				(x) => x.related.page && pageKeyString(x.related.page) === ref.id
			)?.related.page;
			const path = page && pagePath(story, page);
			return path ? `Wiki page ${path}` : `page ${ref.id}`;
		}
	}
}
