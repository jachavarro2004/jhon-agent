package guard

// globSubstWhy is the refusal of a line that turns on zsh's GLOB_SUBST (sh.GlobSubstOn, T228 L2).
const globSubstWhy = "it turns on zsh's GLOB_SUBST, which reads each unquoted expansion as a glob, whose qualifiers may run code the guard does not read"
