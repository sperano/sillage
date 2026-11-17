import 'vitest/browser';

declare module 'vitest/browser' {
	export interface BrowserCommands {
		emulateColorScheme: (colorScheme: 'light' | 'dark' | null) => Promise<void>;
	}
}
