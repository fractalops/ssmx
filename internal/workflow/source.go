package workflow

import (
	"fmt"
	"strings"
)

// SourceKind identifies how a workflow was obtained.
type SourceKind string

const (
	// SourceKindName is a workflow discovered by name from the search path.
	SourceKindName SourceKind = "name"
	// SourceKindFile is a workflow loaded from an explicit file path.
	SourceKindFile SourceKind = "file"
	// SourceKindStdin is a workflow read from stdin.
	SourceKindStdin SourceKind = "stdin"
	// SourceKindDoc is a workflow synthesized from an SSM document reference.
	SourceKindDoc SourceKind = "doc"
)

// SourceMeta carries metadata about how a workflow was resolved.
// It is used for user-facing messages (errors, summaries, dry-run labels).
type SourceMeta struct {
	Kind  SourceKind
	Label string // human-readable label, e.g. "deploy", "/path/wf.yaml", "doc:AWS-RunPatchBaseline"
}

// ResolveWorkflow resolves the active workflow from --run / --run-file flag values.
//
// Resolution order:
//  1. runFile non-empty → load from file (or stdin when "-")
//  2. run starts with "doc:" → synthesize a single-step doc workflow; docParams
//     are baked into Step.Params so callers must pass empty Inputs to RunOptions
//  3. otherwise → discover by name via Load
func ResolveWorkflow(run, runFile string, docParams map[string]string) (*Workflow, *SourceMeta, error) {
	if run == "" && runFile == "" {
		return nil, nil, fmt.Errorf("one of --run or --run-file must be specified")
	}
	if runFile != "" {
		wf, err := LoadFile(runFile)
		if err != nil {
			return nil, nil, err
		}
		kind := SourceKindFile
		label := runFile
		if runFile == "-" {
			kind = SourceKindStdin
			label = "<stdin>"
		}
		return wf, &SourceMeta{Kind: kind, Label: label}, nil
	}
	if strings.HasPrefix(run, "doc:") {
		docName := strings.TrimPrefix(run, "doc:")
		if docName == "" {
			return nil, nil, fmt.Errorf("--run %q: doc name must not be empty (expected doc:<name>)", run)
		}
		wf := SynthesizeDocWorkflow(run, docParams)
		return wf, &SourceMeta{Kind: SourceKindDoc, Label: run}, nil
	}
	wf, err := Load(run)
	if err != nil {
		return nil, nil, err
	}
	return wf, &SourceMeta{Kind: SourceKindName, Label: run}, nil
}

// SynthesizeDocWorkflow builds a single-step workflow from an SSM document
// reference. The docRef should be "doc:<name-or-alias>". docParams map
// directly to the step's ssm-doc params and are deep-copied.
//
// The synthesized workflow has no declared inputs: parameter values are baked
// into Step.Params at synthesis time. Callers must pass empty Inputs to
// RunOptions to avoid ApplyInputs rejecting unknown keys.
func SynthesizeDocWorkflow(docRef string, docParams map[string]string) *Workflow {
	docName := strings.TrimPrefix(docRef, "doc:")
	params := make(map[string]string, len(docParams))
	for k, v := range docParams {
		params[k] = v
	}
	return &Workflow{
		Name: docRef,
		Steps: map[string]*Step{
			"run-doc": {
				SSMDoc: docName,
				Params: params,
			},
		},
	}
}
