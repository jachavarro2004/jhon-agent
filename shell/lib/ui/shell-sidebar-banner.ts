import { visibleWidth } from "@earendil-works/pi-tui";
import type { ShellBarTheme } from "./shell-bar.ts";

export const SIDEBAR_BRAND = "力 Jhon";

export function renderSidebarBanner(theme: ShellBarTheme, width: number): string[] {
	const label = SIDEBAR_BRAND;
	const space = width - visibleWidth(label);
	if (space < 0) return [];
	// The mark takes the theme's accent (the palette's primary), the name its text color.
	const title = theme.fg("accent", "力") + " " + theme.fg("text", "Jhon");
	return [" ".repeat(Math.floor(space / 2)) + title + " ".repeat(Math.ceil(space / 2))];
}
