<script lang="ts">
	import { untrack } from 'svelte';
	import type { Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import type { TimelineItem } from '../timeline';
	import { eventLink, runLink, checkURL, type LinkConfig, type SourceLink } from '../links';
	import { accountLabel, itemLabel } from '../describe';
	import { uncertainAttributions } from '../attribution';
	import { eventKey } from '../keys';
	import ItemTime from './ItemTime.svelte';

	let {
		story,
		items,
		selection,
		links,
		timeZone
	}: {
		story: Story;
		items: TimelineItem[];
		selection: Selection;
		links: LinkConfig;
		timeZone: string;
	} = $props();

	// bind:this sets an entry to null when its row goes.
	const buttons: (HTMLButtonElement | null)[] = $state([]);

	const selectedIndex = $derived(items.findIndex((i) => selection.isSelected(i.ref)));
	// The one row Tab reaches: the selected one, else the first.
	const tabIndex = $derived(Math.max(selectedIndex, 0));
	const uncertain = $derived(uncertainAttributions(story));

	// A new selection, made here or elsewhere (the graph), brings its row
	// into view without taking the focus; rows binding again do not.
	$effect(() => {
		const index = selectedIndex;
		untrack(() => buttons[index]?.scrollIntoView({ block: 'nearest' }));
	});

	function sourceOf(item: TimelineItem): SourceLink | undefined {
		switch (item.kind) {
			case 'event':
				return eventLink(story, item.event, links);
			case 'run':
				return runLink(item.run, links);
			case 'check': {
				const href = checkURL(item.pr, item.check);
				return href === undefined ? undefined : { href, label: `GitHub check ${item.check.id}` };
			}
			case 'phase':
				// A pod reported its markers to sillage: no source has a page for them.
				return undefined;
		}
	}

	/** The row a key moves the selection to, or undefined for a key the list leaves alone. */
	function target(event: KeyboardEvent, index: number): number | undefined {
		if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
			return undefined;
		}
		switch (event.key) {
			case 'ArrowDown':
				return Math.min(index + 1, items.length - 1);
			case 'ArrowUp':
				return Math.max(index - 1, 0);
			case 'Home':
				return 0;
			case 'End':
				return items.length - 1;
		}
		return undefined;
	}

	function onkeydown(event: KeyboardEvent, index: number) {
		const to = target(event, index);
		const item = to === undefined ? undefined : items[to];
		if (to === undefined || !item) {
			return;
		}
		event.preventDefault();
		selection.select(item.ref);
		buttons[to]?.focus();
	}
</script>

<section class="event-list surface" aria-labelledby="event-list-heading">
	<h2 id="event-list-heading">Events</h2>
	<p class="hint" id="event-list-hint">
		Up and down arrows move the selection, Home and End go to the first and last. Each source link
		is also in the detail panel.
	</p>
	<ol aria-describedby="event-list-hint">
		{#each items as item, index (`${item.ref.kind}:${item.ref.id}`)}
			{@const source = sourceOf(item)}
			{@const selected = index === selectedIndex}
			<li class:selected>
				<button
					type="button"
					bind:this={buttons[index]}
					tabindex={index === tabIndex ? 0 : -1}
					aria-current={selected ? 'true' : undefined}
					onclick={() => selection.select(item.ref)}
					onkeydown={(e) => onkeydown(e, index)}
				>
					<span class="when"><ItemTime {item} {timeZone} /></span>
					<span class="what">{itemLabel(item)}</span>
					{#if item.kind === 'event'}
						{@const why = uncertain.get(eventKey(item.event))}
						<span class="who">
							{accountLabel(item.event.actor)}
							{#if item.event.executor}
								<span class="executor"
									>executor {item.event.executor.name} ({item.event.executor.kind})</span
								>
							{/if}
							{#if why}
								<span class="uncertain" title={why}>attribution uncertain</span>
							{/if}
						</span>
					{:else if item.kind === 'run' && item.run.agent}
						<span class="who">agent {item.run.agent}</span>
					{/if}
				</button>
				{#if source}
					<a class="source" href={source.href} tabindex="-1" rel="noreferrer">{source.label}</a>
				{/if}
			</li>
		{/each}
	</ol>
</section>

<style>
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
	}

	li {
		border-block-end: 1px solid var(--color-border);
		padding: 0.25rem 0;
	}

	li.selected {
		background: color-mix(in srgb, var(--color-selected) 20%, var(--color-surface));
	}

	button {
		display: flex;
		flex-direction: column;
		gap: 0.1rem;
		width: 100%;
		padding: 0.25rem 0.5rem;
		border: none;
		background: none;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}

	button:focus-visible {
		outline-offset: -2px;
	}

	.when,
	.who,
	.source,
	.hint {
		font-size: 0.875em;
	}

	.what {
		font-weight: 600;
	}

	.executor,
	.uncertain {
		margin-inline-start: 0.5em;
	}

	.uncertain {
		font-style: italic;
	}

	.source {
		margin-inline-start: 0.5rem;
	}
</style>
