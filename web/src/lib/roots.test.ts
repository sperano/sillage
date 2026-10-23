import { expect, test } from 'vitest';
import type { Root } from './model';
import { rootLabel, rootSegments, sameRoot } from './roots';

const task: Root = { task: { instance: 'vikunja.example.com', id: 102 } };
const page: Root = { page: { instance: 'wiki.example.com', id: 41, locale: 'en' } };

test('sameRoot compares every part of the key', () => {
	expect(sameRoot(task, { task: { instance: 'vikunja.example.com', id: 102 } })).toBe(true);
	expect(sameRoot(task, { task: { instance: 'vikunja.example.com', id: 103 } })).toBe(false);
	expect(sameRoot(task, { task: { instance: 'other.example.com', id: 102 } })).toBe(false);
	expect(sameRoot(page, { page: { instance: 'wiki.example.com', id: 41, locale: 'fr' } })).toBe(
		false
	);
	expect(sameRoot(task, page)).toBe(false);
});

test('rootLabel names the task or the page', () => {
	expect(rootLabel(task)).toBe('task 102 on vikunja.example.com');
	expect(rootLabel(page)).toBe('page 41 (en) on wiki.example.com');
});

test('rootSegments encodes each part', () => {
	expect(rootSegments(task)).toBe('/task/vikunja.example.com/102');
	expect(
		rootSegments({ page: { instance: 'wiki.example.com:8443', id: 7, locale: 'pt/br' } })
	).toBe('/page/wiki.example.com%3A8443/7/pt%2Fbr');
	expect(() => rootSegments({})).toThrow(TypeError);
});
