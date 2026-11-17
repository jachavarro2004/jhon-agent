package render

// themeName is the pi theme Jhon's pi wears: rose ink. The render seeds
// <pi home>/themes/rose-ink.json; jhon-shell rewrites it with `jhon palette
// --write-pi` when the desktop palette it follows changes (only with
// integrations.theme.palette = "desktop"), and pi hot-reloads it.
const themeName = "rose-ink"

// Theme is the theme file's text now. Rose ink (internal/palette) is an
// opt-in integration the renderer never imports (T55): the cli sets Theme,
// as it sets Commands. Unset, the render writes no theme file.
var Theme func() string
