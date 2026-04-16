// Package workflow implements the ssmx workflow DSL: parsing, validation,
// expression resolution, DAG execution, and shell step dispatch.
package workflow

import "fmt"

// Workflow is the parsed form of a .ssmx/workflows/*.yaml file.
type Workflow struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Version     string            `yaml:"version,omitempty"`
	Targets     *Targets          `yaml:"targets,omitempty"`
	Inputs      map[string]*Input `yaml:"inputs,omitempty"`
	Secrets     []*Secret         `yaml:"secrets,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Steps       map[string]*Step  `yaml:"steps"`
	Outputs     map[string]string `yaml:"outputs,omitempty"`
}

// Targets defines default fleet targeting for a workflow.
type Targets struct {
	Tags           map[string]string `yaml:"tags,omitempty"`
	InstanceIDs    []string          `yaml:"instance-ids,omitempty"`
	MaxConcurrency int               `yaml:"max-concurrency,omitempty"`
}

// Input declares a typed workflow parameter.
type Input struct {
	Type        string `yaml:"type"` // "string", "int", "bool"
	Required    bool   `yaml:"required,omitempty"`
	Default     any    `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Secret declares an SSM Parameter Store reference.
type Secret struct {
	Name    string `yaml:"name"`
	SSM     string `yaml:"ssm"`
	Decrypt bool   `yaml:"decrypt,omitempty"`
}

// Step is one unit of work within a workflow.
// Exactly one of Shell, SSMDoc, Workflow, or Parallel must be set.
type Step struct {
	// Discriminated step kind — exactly one must be non-zero.
	Shell    string           `yaml:"shell,omitempty"`
	SSMDoc   string           `yaml:"ssm-doc,omitempty"`
	Workflow string           `yaml:"workflow,omitempty"`
	Parallel map[string]*Step `yaml:"parallel,omitempty"`

	// Shared execution fields.
	Needs   []string          `yaml:"needs,omitempty"`
	If      string            `yaml:"if,omitempty"`
	Always  bool              `yaml:"always,omitempty"`
	Timeout string            `yaml:"timeout,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Outputs map[string]string `yaml:"outputs,omitempty"`

	// ssm-doc specific.
	Params map[string]string `yaml:"params,omitempty"`

	// workflow-step specific.
	With      map[string]any `yaml:"with,omitempty"`
	OnFailure *OnFailure     `yaml:"on-failure,omitempty"`
}

// OnFailure specifies a rollback workflow to run if a step fails.
type OnFailure struct {
	Workflow string         `yaml:"workflow"`
	With     map[string]any `yaml:"with,omitempty"`
}

// Kind returns the kind field name that is set ("shell", "ssm-doc", "workflow",
// "parallel"), or "" if no kind is set.
func (s *Step) Kind() string {
	switch {
	case s.Shell != "":
		return "shell"
	case s.SSMDoc != "":
		return "ssm-doc"
	case s.Workflow != "":
		return "workflow"
	case s.Parallel != nil:
		return "parallel"
	}
	return ""
}

// Validate checks that the workflow is structurally valid: each step has
// exactly one kind field set.
func (wf *Workflow) Validate() error {
	if wf.Targets != nil && len(wf.Targets.Tags) > 0 && len(wf.Targets.InstanceIDs) > 0 {
		return fmt.Errorf("targets: tags and instance-ids are mutually exclusive")
	}
	for name, step := range wf.Steps {
		kinds := 0
		if step.Shell != "" {
			kinds++
		}
		if step.SSMDoc != "" {
			kinds++
		}
		if step.Workflow != "" {
			kinds++
		}
		if step.Parallel != nil {
			kinds++
		}
		if kinds == 0 {
			return fmt.Errorf("step %q has no kind (shell, ssm-doc, workflow, or parallel)", name)
		}
		if kinds > 1 {
			return fmt.Errorf("step %q has more than one kind field set", name)
		}

		// on-failure is only meaningful on workflow: steps.
		if step.OnFailure != nil && step.Workflow == "" {
			return fmt.Errorf("step %q: on-failure is only valid on workflow: steps", name)
		}
		// on-failure must name a rollback workflow.
		if step.OnFailure != nil && step.OnFailure.Workflow == "" {
			return fmt.Errorf("step %q: on-failure must specify a workflow name", name)
		}

		if step.Parallel != nil {
			for subName, subStep := range step.Parallel {
				// Reject nested parallel.
				if subStep.Parallel != nil {
					return fmt.Errorf("parallel sub-step %q in %q: nested parallel is not supported", subName, name)
				}
				// Reject dependency and conditional fields (meaningless inside parallel).
				if len(subStep.Needs) > 0 {
					return fmt.Errorf("parallel sub-step %q in %q: needs: is not supported inside parallel", subName, name)
				}
				if subStep.If != "" {
					return fmt.Errorf("parallel sub-step %q in %q: if: is not supported inside parallel", subName, name)
				}
				if subStep.Always {
					return fmt.Errorf("parallel sub-step %q in %q: always: is not supported inside parallel", subName, name)
				}
				// Validate kind — exactly one of shell/ssm-doc/workflow.
				subKinds := 0
				if subStep.Shell != "" {
					subKinds++
				}
				if subStep.SSMDoc != "" {
					subKinds++
				}
				if subStep.Workflow != "" {
					subKinds++
				}
				if subKinds == 0 {
					return fmt.Errorf("parallel sub-step %q in %q has no kind", subName, name)
				}
				if subKinds > 1 {
					return fmt.Errorf("parallel sub-step %q in %q has more than one kind", subName, name)
				}
			}
		}
	}
	return nil
}

// ApplyInputs validates that all required inputs are provided, applies
// defaults for omitted optional inputs, and returns the resolved input map.
func (wf *Workflow) ApplyInputs(provided map[string]string) (map[string]string, error) {
	// Reject unknown input keys (catches typos before silent discard).
	for name := range provided {
		if _, ok := wf.Inputs[name]; !ok {
			return nil, fmt.Errorf("unknown input %q (not declared in workflow inputs)", name)
		}
	}
	resolved := make(map[string]string, len(wf.Inputs))
	for name, input := range wf.Inputs {
		if v, ok := provided[name]; ok {
			resolved[name] = v
			continue
		}
		if input.Required {
			return nil, fmt.Errorf("required input %q not provided (use --param %s=<value>)", name, name)
		}
		if input.Default != nil {
			resolved[name] = fmt.Sprintf("%v", input.Default)
		}
	}
	return resolved, nil
}
