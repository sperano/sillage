// The bodies the Go handlers answer the client's searches, resolutions and
// freshness requests with (testdata/fixtures/api, internal/api's
// TestResponsesGolden), keyed by the path and query asked for: the client's
// tests answer from them, so a shape the server changed fails here.

import golden from '../../../../testdata/fixtures/api/responses.golden.json';

const bodies: Readonly<Record<string, unknown>> = golden;

/** The paths the golden file has a body for. */
export const goldenPaths: readonly string[] = Object.keys(bodies);

/** A copy of the body the server answers path with; throws when the file has none. */
export function goldenResponse(path: string): unknown {
	if (!Object.hasOwn(bodies, path)) throw new Error(`no golden response for ${path}`);
	return structuredClone(bodies[path]);
}
