// Keyboard activation for the timeline's selectable marks, which are SVG
// groups with the button role.

/** The key and focus handlers of a mark with the button role. */
export interface Activation {
	onkeydown: (e: KeyboardEvent) => void;
	onkeyup: (e: KeyboardEvent) => void;
	onblur: () => void;
}

/**
 * Handlers that run activate as a button does: on Enter's keydown, not its
 * auto-repeats, and on the keyup of a Space pressed on the mark itself, so
 * that holding either runs it once and a Space pressed elsewhere does not.
 * Both keys keep the page from scrolling.
 */
export function activation(activate: () => void): Activation {
	let spaceDown = false;
	return {
		onkeydown: (e) => {
			if (e.key !== 'Enter' && e.key !== ' ') return;
			e.preventDefault();
			if (e.key === ' ') spaceDown = true;
			else if (!e.repeat) activate();
		},
		onkeyup: (e) => {
			if (e.key !== ' ' || !spaceDown) return;
			spaceDown = false;
			e.preventDefault();
			activate();
		},
		onblur: () => {
			spaceDown = false;
		}
	};
}
