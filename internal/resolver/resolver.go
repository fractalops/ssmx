// Package resolver resolves instance targets from names, tags, and bookmarks.
package resolver

import (
	"fmt"
	"strings"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// ErrAmbiguous is returned when multiple instances match the target.
type ErrAmbiguous struct {
	Target  string
	Matches []awsclient.Instance
}

func (e *ErrAmbiguous) Error() string {
	return fmt.Sprintf("%q matches %d instances", e.Target, len(e.Matches))
}

// ErrNotFound is returned when no instance matches the target.
type ErrNotFound struct {
	Target string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("no instance found matching %q", e.Target)
}

// Resolve finds the single Instance that best matches target, consulting
// aliases first, then EC2 Name tags, then instance IDs.
//
// Resolution order:
//  1. Exact alias match (from aliases map)
//  2. Exact Name-tag match (case-insensitive)
//  3. Prefix Name-tag match
//  4. Instance ID match (i-*)
//
// Returns ErrAmbiguous if more than one instance matches.
// Returns ErrNotFound if nothing matches.
func Resolve(target string, instances []awsclient.Instance, aliases map[string]string) (*awsclient.Instance, error) {
	// 1. Alias lookup.
	if aliases != nil {
		if id, ok := aliases[target]; ok {
			for _, inst := range instances {
				if inst.InstanceID == id {
					return &inst, nil
				}
			}
		}
	}

	// 2. Exact Name-tag match (case-insensitive).
	var exact []awsclient.Instance
	lower := strings.ToLower(target)
	for _, inst := range instances {
		if strings.ToLower(inst.Name) == lower {
			exact = append(exact, inst)
		}
	}
	if len(exact) == 1 {
		return &exact[0], nil
	}
	if len(exact) > 1 {
		return nil, &ErrAmbiguous{Target: target, Matches: exact}
	}

	// 3. Prefix Name-tag match.
	var prefix []awsclient.Instance
	for _, inst := range instances {
		if strings.HasPrefix(strings.ToLower(inst.Name), lower) {
			prefix = append(prefix, inst)
		}
	}
	if len(prefix) == 1 {
		return &prefix[0], nil
	}
	if len(prefix) > 1 {
		return nil, &ErrAmbiguous{Target: target, Matches: prefix}
	}

	// 4. Instance ID match.
	for _, inst := range instances {
		if inst.InstanceID == target {
			return &inst, nil
		}
	}

	return nil, &ErrNotFound{Target: target}
}
