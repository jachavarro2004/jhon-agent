package sandbox

// ProjectEnv names the variable that pins a session's project (T128c,
// L894): `jhon start` sets it to the folder that holds `.git` before it
// replaces itself with the runtime, and every wrap of that session passes it
// as `jhon sandbox run --project`, so a command that removed the project's
// `.git` entry does not widen the next one to an enclosing repository.
const ProjectEnv = "JHON_SANDBOX_PROJECT"
