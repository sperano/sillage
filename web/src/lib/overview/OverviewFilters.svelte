<!-- The overview's window, filters and grouping. Applying them changes the
     page's URL, so that a filtered overview is a link and back undoes it. -->
<script lang="ts">
	import type { Router } from '../router.svelte';
	import { MAX_SEARCH_LENGTH } from '../api';
	import { OVERVIEW_PATH, overviewHref, type Grouping, type OverviewQuery } from './query';

	interface Props {
		router: Router;
		/** Its window's fields are empty when it names none: the day before now. */
		query: OverviewQuery;
		/** The agent profiles of the loaded rows, offered for the executor field. */
		executors: readonly string[];
	}

	let { router, query, executors }: Props = $props();

	const id = $props.id();
	/** A datetime-local value, to the minute: 2026-03-10T14:00. */
	const LOCAL_LENGTH = 'YYYY-MM-DDTHH:MM'.length;

	/** An RFC 3339 time as a datetime-local value in UTC. */
	function fieldValue(time: string | undefined): string {
		if (time === undefined) return '';
		const ms = Date.parse(time);
		return Number.isNaN(ms) ? '' : new Date(ms).toISOString().slice(0, LOCAL_LENGTH);
	}

	/** A datetime-local value, read as UTC, as RFC 3339; undefined when empty. */
	function fieldTime(value: FormDataEntryValue | null): string | undefined {
		return typeof value === 'string' && value !== '' ? `${value}:00Z` : undefined;
	}

	function text(value: FormDataEntryValue | null): string {
		return typeof value === 'string' ? value.trim() : '';
	}

	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		const form = new FormData(event.currentTarget);
		const next: OverviewQuery = {
			from: fieldTime(form.get('from')),
			to: fieldTime(form.get('to')),
			project: text(form.get('project')),
			task: text(form.get('task')),
			executor: text(form.get('executor')),
			review: form.has('review'),
			failed: form.has('failed'),
			missing: form.has('missing'),
			group: (form.get('group') === 'executor' ? 'executor' : 'task') satisfies Grouping
		};
		router.navigate(overviewHref(next));
	}
</script>

<form class="filters" onsubmit={submit} aria-label="Window and filters">
	<fieldset>
		<legend>Window, UTC (empty: the day before now)</legend>
		<label>From <input type="datetime-local" name="from" value={fieldValue(query.from)} /></label>
		<label>To <input type="datetime-local" name="to" value={fieldValue(query.to)} /></label>
	</fieldset>
	<fieldset>
		<legend>Filters</legend>
		<label
			>Project <input name="project" value={query.project} maxlength={MAX_SEARCH_LENGTH} /></label
		>
		<label>Task <input name="task" value={query.task} maxlength={MAX_SEARCH_LENGTH} /></label>
		<label
			>Executor <input
				name="executor"
				value={query.executor}
				list="{id}-executors"
				maxlength={MAX_SEARCH_LENGTH}
			/></label
		>
		<datalist id="{id}-executors">
			{#each executors as e (e)}<option value={e}></option>{/each}
		</datalist>
		<label><input type="checkbox" name="review" checked={query.review} /> Review requested</label>
		<label><input type="checkbox" name="failed" checked={query.failed} /> Failed run</label>
		<label><input type="checkbox" name="missing" checked={query.missing} /> Missing history</label>
	</fieldset>
	<fieldset>
		<legend>Group</legend>
		<label
			><input type="radio" name="group" value="task" checked={query.group === 'task'} /> By task</label
		>
		<label
			><input type="radio" name="group" value="executor" checked={query.group === 'executor'} /> By executor</label
		>
	</fieldset>
	<div class="actions">
		<button type="submit">Apply</button>
		<a href={OVERVIEW_PATH}>Reset</a>
	</div>
</form>

<style>
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		align-items: end;
		font-size: 0.8rem;
	}
	fieldset {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
		border: 1px solid #d9e0e8;
		border-radius: 0.4rem;
	}
	label {
		display: inline-flex;
		gap: 0.25rem;
		align-items: center;
	}
	input:not([type]) {
		width: 8rem;
	}
	.actions {
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}
</style>
