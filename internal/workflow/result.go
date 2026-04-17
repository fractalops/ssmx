package workflow

// RunSummary captures the outcome of a single workflow execution on one instance.
type RunSummary struct {
	Workflow string            `json:"workflow"`
	Instance string            `json:"instance"`
	Success  bool              `json:"success"`
	Outputs  map[string]string `json:"outputs,omitempty"`
	Steps    []StepSummary     `json:"steps"`
	Error    string            `json:"error,omitempty"`
}

// StepSummary captures the outcome of a single step execution.
type StepSummary struct {
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	Skipped  bool   `json:"skipped"`
	Exit     int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	DocName  string `json:"doc,omitempty"`
	DocAlias string `json:"doc_alias,omitempty"`
}

// FleetRunSummary captures the aggregate outcome of a fleet workflow execution.
type FleetRunSummary struct {
	Workflow  string       `json:"workflow"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Total     int          `json:"total"`
	Instances []RunSummary `json:"instances"`
}
