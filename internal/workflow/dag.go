package workflow

import (
	"fmt"
	"sort"
)

// Levels returns groups of step names that can execute concurrently.
// Steps within the same group have no dependency on each other.
// All steps in a group must complete before the next group starts.
//
// Uses Kahn's topological sort algorithm. Returns an error if any
// needs reference is undefined or a dependency cycle is detected.
// Returns nil, nil for empty input.
func Levels(steps map[string]*Step) ([][]string, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	// Validate all needs references exist, including self-references.
	for name, step := range steps {
		for _, dep := range step.Needs {
			if dep == name {
				return nil, fmt.Errorf("step %q depends on itself", name)
			}
			if _, ok := steps[dep]; !ok {
				return nil, fmt.Errorf("step %q needs undefined step %q", name, dep)
			}
		}
	}

	// dependents[x] = list of steps that have x in their needs.
	dependents := make(map[string][]string, len(steps))
	for name, step := range steps {
		for _, dep := range step.Needs {
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// inDeg[name] = number of unresolved dependencies for this step.
	inDeg := make(map[string]int, len(steps))
	for name, step := range steps {
		inDeg[name] = len(step.Needs)
	}

	var levels [][]string
	processed := 0

	for processed < len(steps) {
		// Collect steps with no unresolved deps. Deleting from inDeg marks them
		// as scheduled (visited); per Go spec, deleting map keys during range is
		// safe — unvisited keys that get deleted simply won't appear in later
		// iterations.
		var ready []string
		for name, deg := range inDeg {
			if deg == 0 {
				ready = append(ready, name)
				delete(inDeg, name)
			}
		}
		if len(ready) == 0 {
			// Any steps remaining in inDeg are part of a cycle.
			cyclic := make([]string, 0, len(inDeg))
			for name := range inDeg {
				cyclic = append(cyclic, name)
			}
			sort.Strings(cyclic)
			return nil, fmt.Errorf("workflow contains a dependency cycle involving: %v", cyclic)
		}
		sort.Strings(ready) // deterministic ordering
		processed += len(ready)

		// Reduce in-degree for downstream steps.
		for _, name := range ready {
			for _, dep := range dependents[name] {
				inDeg[dep]--
			}
		}

		levels = append(levels, ready)
	}
	return levels, nil
}
