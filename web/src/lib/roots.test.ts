import { expect, test } from 'vitest';
import type { Root } from './model';
import { rootLabel, rootName, rootSegments, sameRoot } from './roots';

const task: Root = { task: { instance: 'vikunja.example.com', id: 102 } };

test('sameRoot compares every part of the key', () => {
	expect(sameRoot(task, { task: { instance: 'vikunja.example.com', id: 102 } })).toBe(true);
	expect(sameRoot(task, { task: { instance: 'vikunja.example.com', id: 103 } })).toBe(false);
	expect(sameRoot(task, { task: { instance: 'other.example.com', id: 102 } })).toBe(false);
});

test('rootLabel names the task', () => {
	expect(rootLabel(task)).toBe('task 102 on vikunja.example.com');
});

test('rootSegments encodes each part', () => {
	expect(rootSegments(task)).toBe('/task/vikunja.example.com/102');
});

test('rootName names the task without its instance', () => {
	expect(rootName(task)).toBe('task 102');
});
