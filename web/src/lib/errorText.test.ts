import { expect, test } from 'vitest';
import { HttpError, InvalidResponseError, NetworkError, NotFoundError, TimeoutError } from './api';
import { errorText } from './errorText';

const REQUEST = 'GET /api/v1/coverage';
const NOT_FOUND = 'Nothing here.';

test.each<[string, unknown, string]>([
	['a 404', new NotFoundError(REQUEST, 404, { title: 'Not Found' }), NOT_FOUND],
	[
		'an error status with a problem',
		new HttpError(REQUEST, 503, { title: 'Service Unavailable', detail: 'the store is down' }),
		"Sillage's server answered with an error (503 Service Unavailable): the store is down."
	],
	[
		'an error status alone',
		new HttpError(REQUEST, 500, {}),
		"Sillage's server answered with an error (500)."
	],
	[
		'a timeout',
		new TimeoutError(REQUEST, 15_000),
		"Sillage's server did not answer within 15 seconds."
	],
	[
		'a network failure',
		new NetworkError(REQUEST, 'down'),
		"Sillage's server could not be reached."
	],
	[
		'an invalid response',
		new InvalidResponseError(REQUEST, 'bad'),
		"Sillage's server sent an answer this page cannot read. If Sillage was just updated, reloading the page may help."
	],
	['anything else', new Error('bug'), 'Something went wrong in this page.']
])('%s', (_, err, want) => {
	expect(errorText(err, NOT_FOUND)).toBe(want);
});
