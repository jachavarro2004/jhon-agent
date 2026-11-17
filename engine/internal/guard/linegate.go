package guard

// LineIsPlain is whether line passes the WU1 allowlist gate (lineFacts,
// L796): it parses, defines no function, runs no state-changing builtin
// (exec, alias, shopt, source, eval …), makes no uppercase, indexed or
// arithmetic assignment, has no arithmetic context, and every program word
// is a plain unquoted literal. Such a line cannot change what a program
// name runs or where a statement's output goes, beyond its own redirects
// (T109 WU3, L808).
func LineIsPlain(line string) bool {
	return lineFacts(line) != nil
}
