package cmd

import "testing"

func TestParseRootArgs_NoArgs_NoFlag(t *testing.T) {
	result := parseRootArgs(false, false, false, []string{}, -1)
	if result.action != actionHelp {
		t.Errorf("expected actionHelp, got %v", result.action)
	}
}

func TestParseRootArgs_InteractiveFlag(t *testing.T) {
	result := parseRootArgs(true, false, false, []string{}, -1)
	if result.action != actionPicker {
		t.Errorf("expected actionPicker, got %v", result.action)
	}
}

func TestParseRootArgs_InteractiveFlagWithTarget_StillPicker(t *testing.T) {
	// -i takes precedence over a positional arg
	result := parseRootArgs(true, false, false, []string{"web-prod"}, -1)
	if result.action != actionPicker {
		t.Errorf("expected actionPicker, got %v", result.action)
	}
}

func TestParseRootArgs_TargetOnly(t *testing.T) {
	result := parseRootArgs(false, false, false, []string{"web-prod"}, -1)
	if result.action != actionConnect {
		t.Errorf("expected actionConnect, got %v", result.action)
	}
	if result.target != "web-prod" {
		t.Errorf("expected target 'web-prod', got %q", result.target)
	}
	if len(result.remoteCmd) != 0 {
		t.Errorf("expected no remoteCmd, got %v", result.remoteCmd)
	}
}

func TestParseRootArgs_TargetWithDashDash(t *testing.T) {
	// ssmx web-prod -- df -h
	// cobra presents args as ["web-prod", "df", "-h"], dashAt=1
	result := parseRootArgs(false, false, false, []string{"web-prod", "df", "-h"}, 1)
	if result.action != actionExec {
		t.Errorf("expected actionExec, got %v", result.action)
	}
	if result.target != "web-prod" {
		t.Errorf("expected target 'web-prod', got %q", result.target)
	}
	if len(result.remoteCmd) != 2 || result.remoteCmd[0] != "df" || result.remoteCmd[1] != "-h" {
		t.Errorf("expected remoteCmd [df -h], got %v", result.remoteCmd)
	}
}

func TestParseRootArgs_DashDashWithNoTarget_IsHelp(t *testing.T) {
	// ssmx -- df -h makes no sense, treat as help
	result := parseRootArgs(false, false, false, []string{"df", "-h"}, 0)
	if result.action != actionHelp {
		t.Errorf("expected actionHelp, got %v", result.action)
	}
}

func TestParseRootArgs_ListFlag(t *testing.T) {
	result := parseRootArgs(false, true, false, []string{}, -1)
	if result.action != actionList {
		t.Errorf("expected actionList, got %v", result.action)
	}
}

func TestParseRootArgs_ConfigureFlag(t *testing.T) {
	result := parseRootArgs(false, false, true, []string{}, -1)
	if result.action != actionConfigure {
		t.Errorf("expected actionConfigure, got %v", result.action)
	}
}

func TestParseRootArgs_ConfigureTakesPrecedenceOverList(t *testing.T) {
	// --configure wins if both somehow set
	result := parseRootArgs(false, true, true, []string{}, -1)
	if result.action != actionConfigure {
		t.Errorf("expected actionConfigure, got %v", result.action)
	}
}
