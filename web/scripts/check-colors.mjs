import { readdir, readFile } from 'node:fs/promises';
import { extname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const SOURCE_ROOT = fileURLToPath(new URL('../src/', import.meta.url));
const THEME_FILE = 'app.css';
const SOURCE_EXTENSIONS = new Set(['.css', '.html', '.js', '.svelte', '.ts']);
const HEX_COLOR = /#[\da-f]{3,8}\b/gi;

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
	for (const match of source.matchAll(HEX_COLOR)) {
		const line = source.slice(0, match.index).split('\n').length;
		violations.push(`${name}:${line}: ${match[0]}`);
	}
}

if (violations.length > 0) {
	console.error(`Hex colors must be defined in src/${THEME_FILE}:\n${violations.join('\n')}`);
	process.exitCode = 1;
}
