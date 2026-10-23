/**
 * names' entry for value, or value itself when names has none: a value
 * from a newer model, or one that is a property every object inherits
 * ("constructor"), is shown as it is.
 */
export function named<K extends string, V>(names: Readonly<Record<K, V>>, value: K): V | K {
	return Object.hasOwn(names, value) ? names[value] : value;
}
