/**
 * The environment of a read-only git run: env with optional locks off, so a
 * status or diff never takes .git/index.lock to refresh the index, and one
 * killed by a timeout leaves none behind (T35, L320).
 */
export function gitReadEnv(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
	return { ...env, GIT_OPTIONAL_LOCKS: "0" };
}
