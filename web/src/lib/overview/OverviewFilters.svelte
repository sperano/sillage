<!-- The overview's window, filters and grouping. Applying them changes the
     page's URL, so that a filtered overview is a link and back undoes it. -->
<script lang="ts">
	import { MAX_SEARCH_LENGTH } from '../api';
	import type { Router } from '../router.svelte';
	import {
		OVERVIEW_PATH,
		PARAMS,
		overviewHref,
		queryOfForm,
		timeField,
		type OverviewQuery
	} from './query';

	interface Props {
		router: Router;
		/** Its window's fields are empty when it names none: the day before now. */
		query: OverviewQuery;
		/** The agent profiles of the loaded rows, offered for the executor field. */
		executors: readonly string[];
	}

	let { router, query, executors }: Props = $props();

	const id = $props.id();
	// Counts the resets, so that the form is drawn again with the query's
	// values even when resetting does not change the URL.
	let resets = $state(0);

	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		router.navigate(overviewHref(queryOfForm(new FormData(event.currentTarget))));
	}

	function reset() {
		resets++;
		router.navigate(OVERVIEW_PATH);
	}
</script>

{#key `${overviewHref(query)}#${resets}`}
	<form class="filters" onsubmit={submit} aria-label="Window and filters">
		<fieldset>
			<legend>Window, UTC (empty: the day before the end, now by default)</legend>
			<label
				>From <input
					type="datetime-local"
					step="1"
					name={PARAMS.from}
					value={timeField(query.from)}
				/></label
			>
			<label
				>To <input
					type="datetime-local"
					step="1"
					name={PARAMS.to}
					value={timeField(query.to)}
				/></label
			>
		</fieldset>
		<fieldset>
			<legend>Filters</legend>
			<label
				>Project <input
					name={PARAMS.project}
					value={query.project}
					maxlength={MAX_SEARCH_LENGTH}
				/></label
			>
			<label
				>Task <input name={PARAMS.task} value={query.task} maxlength={MAX_SEARCH_LENGTH} /></label
			>
			<label
				>Executor <input
					name={PARAMS.executor}
					value={query.executor}
					list="{id}-executors"
					maxlength={MAX_SEARCH_LENGTH}
				/></label
			>
			<datalist id="{id}-executors">
				{#each executors as e (e)}<option value={e}></option>{/each}
			</datalist>
			<label
				><input type="checkbox" name={PARAMS.review} checked={query.review} /> Review requested</label
			>
			<label><input type="checkbox" name={PARAMS.failed} checked={query.failed} /> Failed run</label
			>
			<label
				><input type="checkbox" name={PARAMS.missing} checked={query.missing} /> Missing history</label
			>
		</fieldset>
		<fieldset>
			<legend>Group</legend>
			<label
				><input type="radio" name={PARAMS.group} value="task" checked={query.group === 'task'} /> By task</label
			>
			<label
				><input
					type="radio"
					name={PARAMS.group}
					value="executor"
					checked={query.group === 'executor'}
				/> By executor</label
			>
		</fieldset>
		<div class="actions">
			<button type="submit">Apply</button>
			<button type="button" onclick={reset}>Reset</button>
		</div>
	</form>
{/key}

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
