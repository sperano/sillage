<script lang="ts">
	import type { Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import type { TimelineItem } from '../timeline';
	import { eventLink, runLink, checkURL, type LinkConfig, type SourceLink } from '../links';
	import { accountLabel, itemLabel } from '../describe';
	import { attributionUncertain } from '../attribution';
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

	let buttons: HTMLButtonElement[] = $state([]);

	const selectedIndex = $derived(items.findIndex((i) => selection.isSelected(i.ref)));
	// The one row Tab reaches: the selected one, else the first.
	const tabIndex = $derived(Math.max(selectedIndex, 0));

	// A selection made elsewhere (the graph) brings its row into view
	// without taking the focus.
	$effect(() => {
		buttons[selectedIndex]?.scrollIntoView({ block: 'nearest' });
	});

	function link(item: TimelineItem): SourceLink | undefined {
		switch (item.kind) {
			case 'event':
				return eventLink(story, item.event, links);
			case 'run':
				return runLink(item.run, links);
			case 'check':
				return { href: checkURL(item.pr, item.check), label: `GitHub check ${item.check.id}` };
		}
	}

	function moveTo(index: number) {
		const item = items[index];
		if (!item) {
			return;
		}
		selection.select(item.ref);
		buttons[index]?.focus();
	}

	function onkeydown(event: KeyboardEvent, index: number) {
		const target = {
			ArrowDown: index + 1,
			ArrowUp: index - 1,
			Home: 0,
			End: items.length - 1
		}[event.key];
		if (target === undefined) {
			return;
		}
		event.preventDefault();
		moveTo(Math.min(Math.max(target, 0), items.length - 1));
	}
</script>

<section class="event-list" aria-labelledby="event-list-heading">
	<h2 id="event-list-heading">Events</h2>
	<p class="hint" id="event-list-hint">
		Up and down arrows move the selection, Home and End go to the first and last. Each source link
		is also in the detail panel.
	</p>
	<ol aria-describedby="event-list-hint">
		{#each items as item, index (`${item.ref.kind}:${item.ref.id}`)}
			{@const href = link(item)}
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
						{@const uncertain = attributionUncertain(story, item.event)}
						<span class="who">
							{accountLabel(item.event.actor)}
							{#if item.event.executor}
								<span class="executor"
									>executor {item.event.executor.name} ({item.event.executor.kind})</span
								>
							{/if}
							{#if uncertain}
								<span class="uncertain" title={uncertain}>attribution uncertain</span>
							{/if}
						</span>
					{:else if item.kind === 'run' && item.run.agent}
						<span class="who">agent {item.run.agent}</span>
					{/if}
				</button>
				{#if href}
					<a class="source" href={href.href} tabindex="-1" rel="noreferrer">{href.label}</a>
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
		border-block-end: 1px solid color-mix(in srgb, currentColor 20%, transparent);
		padding: 0.25rem 0;
	}

	li.selected {
		background: color-mix(in srgb, Highlight 20%, transparent);
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
		outline: 2px solid Highlight;
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
