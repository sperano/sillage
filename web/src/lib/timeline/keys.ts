// Keyboard activation for the timeline's selectable marks, which are SVG
// groups with the button role.

/** The key handlers of a mark with the button role. */
export interface Activation {
	onkeydown: (e: KeyboardEvent) => void;
	onkeyup: (e: KeyboardEvent) => void;
}

/**
 * Handlers that run activate as a button does: on Enter's keydown, not its
 * auto-repeats, and on Space's keyup, so that holding either runs it once.
 * Both keys keep the page from scrolling.
 */
export function activation(activate: () => void): Activation {
	return {
		onkeydown: (e) => {
			if (e.key !== 'Enter' && e.key !== ' ') return;
			e.preventDefault();
			if (e.key === 'Enter' && !e.repeat) activate();
		},
		onkeyup: (e) => {
			if (e.key !== ' ') return;
			e.preventDefault();
			activate();
		}
	};
}
