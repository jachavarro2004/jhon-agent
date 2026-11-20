/**
 * shell/lib/ui/primitives/meter.ts — the Meter (design/system.md "Meter"):
 * cells filled in proportion, on one ladder for every meter: ok under 70,
 * warn from 70, bad from 90. The status bar's context meter draws with it.
 */
import { glyph } from "./glyphs.ts";

export const METER_WARN = 70;
export const METER_BAD = 90;
export const METER_CELLS = 8;

/** The meter's pi role at percent: none known is dim. */
export type MeterTone = "success" | "warning" | "error" | "dim";

export function meterTone(percent: number | null): MeterTone {
	if (percent === null) return "dim";
	if (percent >= METER_BAD) return "error";
	if (percent >= METER_WARN) return "warning";
	return "success";
}

/** `▰▰▰▱▱▱▱▱`: filled cells rounded to the nearest; unknown is empty. */
export function renderMeter(percent: number | null, cells = METER_CELLS, ascii = false): string {
	const filled = Math.round((Math.max(0, Math.min(100, percent ?? 0)) / 100) * cells);
	return glyph("meterFilled", ascii).repeat(filled) + glyph("meterEmpty", ascii).repeat(cells - filled);
}

/** The meter painted: the filled cells in its tone, the empty ones in the border role. */
export function paintMeter(percent: number | null, theme: { fg(role: string, text: string): string }, cells = METER_CELLS, ascii = false): string {
	const filled = Math.round((Math.max(0, Math.min(100, percent ?? 0)) / 100) * cells);
	const meter = renderMeter(percent, cells, ascii);
	const split = filled * glyph("meterFilled", ascii).length;
	return theme.fg(meterTone(percent), meter.slice(0, split)) + theme.fg("border", meter.slice(split));
}
