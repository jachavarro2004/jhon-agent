package main

// Record is one phase's numbers, `<out>/<label>.json`. It holds counts,
// times and verdicts only: no command text, no path and no project name (a
// project is its line number in the projects file).
type Record struct {
	Label    string    `json:"label"`
	Created  string    `json:"created"`
	Jhon     JhonInfo  `json:"jhon"`
	Speed    *Speed    `json:"speed,omitempty"`
	Friction *Friction `json:"friction,omitempty"`
	Budgets  Budgets   `json:"budgets,omitempty"`
	Hygiene  *Hygiene  `json:"hygiene,omitempty"`
	// Results are the correctness results the caller recorded (--result).
	Results map[string]string `json:"results,omitempty"`
	// Skipped names each measure not taken and why.
	Skipped map[string]string `json:"skipped,omitempty"`
}

// JhonInfo is what `jhon version --json` says of the measured binary.
type JhonInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Speed is measure 1: per project, each command's times.
type Speed struct {
	Runs     int            `json:"runs"`
	Projects []ProjectSpeed `json:"projects"`
}

// ProjectSpeed is one project's times; Index is its line in the projects
// file, counting only the paths (1 is the first).
type ProjectSpeed struct {
	Index       int               `json:"index"`
	Before      Objects           `json:"objects_before"`
	After       Objects           `json:"objects_after"`
	ObjectsGrew bool              `json:"objects_grew"`
	Commands    map[string]Timing `json:"commands"`
	Skipped     map[string]string `json:"skipped,omitempty"`
}

// Objects counts the files under the project's .git/objects and under its
// git common dir's objects.
type Objects struct {
	Project int `json:"project"`
	Common  int `json:"common"`
}

// Timing is one command's runs: the median and the maximum in
// milliseconds, the runs that hit the timeout and each exit code's count.
type Timing struct {
	Runs      int            `json:"runs"`
	MedianMS  float64        `json:"median_ms"`
	MaxMS     float64        `json:"max_ms"`
	Timeouts  int            `json:"timeouts"`
	ExitCodes map[string]int `json:"exit_codes"`
}

// Friction is measure 2: tool results and guard stops in the runtimes'
// session transcripts.
type Friction struct {
	Since       string               `json:"since"`
	Sessions    int                  `json:"sessions"`
	ToolResults int                  `json:"tool_results"`
	Stops       int                  `json:"stops"`
	Per100      float64              `json:"per_100"`
	PerRuntime  map[string]*Count    `json:"per_runtime"`
	PerGuard    map[string]int       `json:"per_guard"`
	PerClass    map[string]ClassRate `json:"per_class"`
	PerWeek     map[string]*Count    `json:"per_week"`
	// LeftOut are the sessions left out of the rest: "jhon" (a project
	// folder whose name holds jhon: Jhon's own development) and "scratch".
	LeftOut map[string]*Count `json:"left_out"`
}

// Count is sessions, tool results and stops, with the stops per 100 tool
// results.
type Count struct {
	Sessions    int     `json:"sessions"`
	ToolResults int     `json:"tool_results"`
	Stops       int     `json:"stops"`
	Per100      float64 `json:"per_100"`
}

// ClassRate is one rule class's stops and its stops per 100 tool results.
type ClassRate struct {
	Stops  int     `json:"stops"`
	Per100 float64 `json:"per_100"`
}

// Budgets is measure 3: each fixed text's bytes and its limit.
type Budgets map[string]Budget

// Budget is one text's bytes and the limit its test holds it to.
type Budget struct {
	Value int `json:"value"`
	Limit int `json:"limit"`
}

// Hygiene is measure 4: the entries a suite run left in the TMPDIR it was
// given.
type Hygiene struct {
	SuiteTmpLeftovers int `json:"suite_tmp_leftovers"`
}
