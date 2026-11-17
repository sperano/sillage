import { readdir, readFile } from 'node:fs/promises';
import { extname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const SOURCE_ROOT = fileURLToPath(new URL('../src/', import.meta.url));
const THEME_FILE = 'app.css';
const SOURCE_EXTENSIONS = new Set(['.css', '.html', '.js', '.svelte', '.ts']);
const HEX_COLOR = /#[\da-f]{3,8}\b/gi;
// A colour function is a colour too, in what styles the page; tests compare
// computed styles, which the browser reports as rgb(), so they may name one.
const COLOR_FUNCTION = /\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(/gi;
const STYLE_EXTENSIONS = new Set(['.css', '.html', '.svelte']);

async function sourceFiles(directory) {
	const entries = await readdir(directory, { withFileTypes: true });
	const files = await Promise.all(
		entries.map((entry) => {
			const path = join(directory, entry.name);
			return entry.isDirectory() ? sourceFiles(path) : [path];
		})
	);
	return files.flat();
}

const violations = [];
for (const path of await sourceFiles(SOURCE_ROOT)) {
	const name = relative(SOURCE_ROOT, path);
	if (name === THEME_FILE || !SOURCE_EXTENSIONS.has(extname(path))) continue;
	const source = await readFile(path, 'utf8');
	const matches = [...source.matchAll(HEX_COLOR)];
	if (STYLE_EXTENSIONS.has(extname(path))) matches.push(...source.matchAll(COLOR_FUNCTION));
	for (const match of matches) {
		const line = source.slice(0, match.index).split('\n').length;
		violations.push(`${name}:${line}: ${match[0]}`);
	}
}

if (violations.length > 0) {
	console.error(`Colors must be defined in src/${THEME_FILE}:\n${violations.join('\n')}`);
	process.exitCode = 1;
}
