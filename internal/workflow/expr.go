package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ExprContext holds all values available for ${{ }} expression resolution.
type ExprContext struct {
	Inputs  map[string]string
	Secrets map[string]string // nil until fetched (Plan 2)
	Env     map[string]string
	Steps   map[string]*StepResult
	Target  TargetInfo
	// CurrentStdout and CurrentExitCode are only valid when resolving
	// outputs: expressions inside a running step.
	CurrentStdout   string
	CurrentExitCode int
}

// TargetInfo holds per-instance metadata available as ${{ target.* }}.
type TargetInfo struct {
	Name       string
	InstanceID string
	PrivateIP  string
}

// StepResult holds the outcome of a completed step.
type StepResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Success  bool
	Outputs  map[string]string
}

var exprRe = regexp.MustCompile(`\$\{\{\s*([^}]+?)\s*\}\}`)

// Resolve replaces all ${{ expr }} occurrences in s with their resolved
// string values. Returns an error if any expression references an unknown
// or unset field.
func Resolve(s string, ctx ExprContext) (string, error) {
	var resolveErr error
	result := exprRe.ReplaceAllStringFunc(s, func(match string) string {
		if resolveErr != nil {
			return ""
		}
		inner := strings.TrimSpace(exprRe.FindStringSubmatch(match)[1])
		val, err := resolveOne(inner, ctx)
		if err != nil {
			resolveErr = err
			return ""
		}
		return val
	})
	return result, resolveErr
}

// EvalBool resolves all ${{ }} in s then evaluates the result as a boolean.
// Supported forms after resolution:
//   - "true" or "1" → true
//   - anything else → false
//   - "lhs != rhs" → lhs != rhs (string comparison)
//   - "lhs == rhs" → lhs == rhs (string comparison)
func EvalBool(s string, ctx ExprContext) (bool, error) {
	resolved, err := Resolve(s, ctx)
	if err != nil {
		return false, err
	}
	if idx := strings.Index(resolved, " != "); idx >= 0 {
		lhs := strings.TrimSpace(resolved[:idx])
		rhs := strings.TrimSpace(resolved[idx+4:])
		return lhs != rhs, nil
	}
	if idx := strings.Index(resolved, " == "); idx >= 0 {
		lhs := strings.TrimSpace(resolved[:idx])
		rhs := strings.TrimSpace(resolved[idx+4:])
		return lhs == rhs, nil
	}
	return resolved == "true" || resolved == "1", nil
}

func resolveOne(expr string, ctx ExprContext) (string, error) {
	// Split into at most 3 parts on "." to get namespace, name, and rest.
	parts := strings.SplitN(expr, ".", 3)

	switch parts[0] {
	case "inputs":
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid inputs expression: %q", expr)
		}
		v, ok := ctx.Inputs[parts[1]]
		if !ok {
			return "", fmt.Errorf("input %q not set", parts[1])
		}
		return v, nil

	case "secrets":
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid secrets expression: %q", expr)
		}
		if ctx.Secrets == nil {
			return "", fmt.Errorf("secrets not available (no secrets block resolved)")
		}
		v, ok := ctx.Secrets[parts[1]]
		if !ok {
			return "", fmt.Errorf("secret %q not resolved", parts[1])
		}
		return v, nil

	case "env":
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid env expression: %q", expr)
		}
		v, ok := ctx.Env[parts[1]]
		if !ok {
			return "", fmt.Errorf("env %q not set", parts[1])
		}
		return v, nil

	case "steps":
		// steps.<name>.success | exitCode | stdout | outputs.<key>
		if len(parts) < 3 {
			return "", fmt.Errorf("invalid steps expression (need steps.<name>.<field>): %q", expr)
		}
		stepName := parts[1]
		field := parts[2]
		res, ok := ctx.Steps[stepName]
		if !ok {
			return "", fmt.Errorf("step %q not found in context", stepName)
		}
		switch field {
		case "success":
			if res.Success {
				return "true", nil
			}
			return "false", nil
		case "exitCode":
			return strconv.Itoa(res.ExitCode), nil
		case "stdout":
			return res.Stdout, nil
		case "stderr":
			return res.Stderr, nil
		default:
			if strings.HasPrefix(field, "outputs.") {
				key := strings.TrimPrefix(field, "outputs.")
				v, ok := res.Outputs[key]
				if !ok {
					return "", fmt.Errorf("step %q has no output %q", stepName, key)
				}
				return v, nil
			}
		}
		return "", fmt.Errorf("unknown steps field %q in expression %q", field, expr)

	case "target":
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid target expression: %q", expr)
		}
		switch parts[1] {
		case "name":
			return ctx.Target.Name, nil
		case "instance_id":
			return ctx.Target.InstanceID, nil
		case "private_ip":
			return ctx.Target.PrivateIP, nil
		}
		return "", fmt.Errorf("unknown target field %q in expression %q", parts[1], expr)

	case "stdout":
		return ctx.CurrentStdout, nil

	case "exitCode":
		return strconv.Itoa(ctx.CurrentExitCode), nil
	}

	return "", fmt.Errorf("unknown expression namespace %q in %q", parts[0], expr)
}
