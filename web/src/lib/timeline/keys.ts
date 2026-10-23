// Keyboard activation for the timeline's selectable marks, which are SVG
// groups with the button role.

/** A keydown handler that runs activate on Enter or Space, as a button does. */
export function onActivate(activate: () => void): (e: KeyboardEvent) => void {
	return (e) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			activate();
		}
	};
}
