// What a reader is told when a request fails: what failed and what it
// means for what the page shows, in plain words.

import { HttpError, InvalidResponseError, NetworkError, NotFoundError, TimeoutError } from './api';
import { MS_PER_SECOND } from './format';

/** The sentence that tells the reader err happened; notFound is what a 404 means here. */
export function errorText(err: unknown, notFound: string): string {
	if (err instanceof NotFoundError) return notFound;
	if (err instanceof HttpError) {
		const detail = err.problem.detail ? `: ${err.problem.detail}` : '';
		const status = [err.status, err.problem.title].filter(Boolean).join(' ');
		return `Sillage's server answered with an error (${status})${detail}.`;
	}
	if (err instanceof TimeoutError) {
		const seconds = err.timeoutMs / MS_PER_SECOND;
		return `Sillage's server did not answer within ${seconds} seconds.`;
	}
	if (err instanceof NetworkError) return "Sillage's server could not be reached.";
	if (err instanceof InvalidResponseError) {
		return "Sillage's server sent an answer this page cannot read. If Sillage was just updated, reloading the page may help.";
	}
	return 'Something went wrong in this page.';
}
