// WCAG 2 contrast between two colours as the browser computes them: the
// theme's tests read tokens through getComputedStyle and check them here.

/** An sRGB colour, each channel from 0 to 1. */
export type Rgb = readonly [red: number, green: number, blue: number];

/** WCAG AA: normal text against its background. */
export const TEXT_CONTRAST = 4.5;
/** WCAG AA: graphical objects (bars, marks, focus rings) against theirs. */
export const GRAPHIC_CONTRAST = 3;

const CHANNEL_MAX = 255;
/** sRGB's transfer function: below this, a channel is linear. */
const LINEAR_LIMIT = 0.04045;
const LINEAR_SLOPE = 12.92;
const GAMMA_OFFSET = 0.055;
const GAMMA_SCALE = 1.055;
const GAMMA = 2.4;
/** Rec. 709 coefficients of the red, green and blue channels. */
const LUMINANCE_WEIGHTS: Rgb = [0.2126, 0.7152, 0.0722];
/** The flare WCAG adds to both luminances. */
const FLARE = 0.05;

const RGB_FUNCTION = /^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)\s*(?:[,/]\s*([\d.]+)\s*)?\)$/;
const SRGB_FUNCTION = /^color\(srgb\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*(?:\/\s*([\d.]+)\s*)?\)$/;
const OPAQUE = 1;

/**
 * Parses a computed colour: `rgb()`/`rgba()` with 0–255 channels, or
 * `color(srgb …)` with 0–1 channels (what Chromium computes a `color-mix()`
 * to). A translucent colour throws: its contrast depends on what is under it.
 */
export function parseColor(computed: string): Rgb {
	const rgb = RGB_FUNCTION.exec(computed.trim());
	const srgb = rgb ? undefined : SRGB_FUNCTION.exec(computed.trim());
	const match = rgb ?? srgb;
	if (!match) throw new Error(`not an sRGB colour: ${computed}`);
	if (match[4] !== undefined && Number(match[4]) !== OPAQUE) {
		throw new Error(`translucent colour: ${computed}`);
	}
	const scale = rgb ? CHANNEL_MAX : 1;
	return [Number(match[1]) / scale, Number(match[2]) / scale, Number(match[3]) / scale];
}

function linear(channel: number): number {
	return channel <= LINEAR_LIMIT
		? channel / LINEAR_SLOPE
		: ((channel + GAMMA_OFFSET) / GAMMA_SCALE) ** GAMMA;
}

/** WCAG relative luminance, from 0 (black) to 1 (white). */
export function luminance(color: Rgb): number {
	return color.reduce((sum, channel, i) => sum + LUMINANCE_WEIGHTS[i]! * linear(channel), 0);
}

/** WCAG contrast ratio, from 1 (same luminance) to 21 (black on white). */
export function contrast(a: Rgb, b: Rgb): number {
	const [lighter, darker] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (lighter! + FLARE) / (darker! + FLARE);
}
