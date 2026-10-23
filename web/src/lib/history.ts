// Where the router reads and writes the current URL: the browser's history
// (browserHistory), or one kept in memory (memoryHistory) for tests.

/** Where the router reads and writes the current URL. */
export interface History {
	/** The current URL. */
	location(): URL;
	/** Adds href as a new entry. */
	push(href: string): void;
	/** Replaces the current entry with href. */
	replace(href: string): void;
	/** Calls onChange when the user moves through the entries; returns a function that stops it. */
	listen(onChange: () => void): () => void;
}

/** The browser's history: the address bar and its back and forward buttons. */
export function browserHistory(): History {
	return {
		location: () => new URL(window.location.href),
		push: (href) => window.history.pushState(null, '', href),
		replace: (href) => window.history.replaceState(null, '', href),
		listen: (onChange) => {
			window.addEventListener('popstate', onChange);
			return () => window.removeEventListener('popstate', onChange);
		}
	};
}

/** The origin memoryHistory resolves its entries against. */
export const MEMORY_ORIGIN = 'http://sillage.example.com';

/** A history in memory, starting at href: for tests and for no browser. */
export function memoryHistory(href: string): History & { entries(): string[]; back(): void } {
	const entries = [new URL(href, MEMORY_ORIGIN)];
	let index = 0;
	const listeners = new Set<() => void>();
	const current = (): URL => {
		const url = entries[index];
		if (url === undefined) throw new Error(`memory history has no entry ${index}`);
		return url;
	};
	return {
		location: () => new URL(current()),
		push: (to) => {
			entries.splice(index + 1, Infinity, new URL(to, current()));
			index = entries.length - 1;
		},
		replace: (to) => {
			entries[index] = new URL(to, current());
		},
		listen: (onChange) => {
			listeners.add(onChange);
			return () => listeners.delete(onChange);
		},
		entries: () => entries.map((u) => u.pathname + u.search),
		back: () => {
			index = Math.max(0, index - 1);
			listeners.forEach((l) => l());
		}
	};
}
