package workflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// stdinReader is the source used when Load is called with name "-".
// Overridden in tests to inject fake stdin content.
var stdinReader io.Reader = os.Stdin

// Load finds and parses a workflow by name. Project-local
// (.ssmx/workflows/<name>.yaml) takes precedence over personal
// (~/.ssmx/workflows/<name>.yaml).
// Pass "-" as name to read from stdin.
func Load(name string) (*Workflow, error) {
	if name == "-" {
		if f, ok := stdinReader.(*os.File); ok {
			if term.IsTerminal(int(f.Fd())) {
				return nil, fmt.Errorf("--run -: stdin is a terminal; pipe a workflow YAML or redirect a file")
			}
		}
		return loadFromReader(stdinReader, "<stdin>")
	}
	dirs, err := searchDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, name+".yaml")
		data, readErr := os.ReadFile(p)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading %s: %w", p, readErr)
		}
		var wf Workflow
		if err := yaml.Unmarshal(data, &wf); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		if err := wf.Validate(); err != nil {
			return nil, fmt.Errorf("validating %s: %w", p, err)
		}
		return &wf, nil
	}
	return nil, fmt.Errorf("workflow %q not found (searched %s)", name, strings.Join(dirs, ", "))
}

// List returns all available workflow names, deduplicated with project-local
// taking precedence over personal on name collision.
func List() ([]string, error) {
	dirs, err := searchDirs()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var names []string
	for _, dir := range dirs {
		entries, readErr := os.ReadDir(dir)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, readErr)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			n := strings.TrimSuffix(e.Name(), ".yaml")
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names, nil
}

// searchDirs returns [projectLocal, personalGlobal] in precedence order.
// Non-existent directories are included; callers skip them with os.IsNotExist.
func searchDirs() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting cwd: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}
	return []string{
		filepath.Join(projectRoot(cwd), ".ssmx", "workflows"),
		filepath.Join(home, ".ssmx", "workflows"),
	}, nil
}

// loadFromReader parses and validates a workflow from r. source is used in
// error messages (e.g. "<stdin>").
func loadFromReader(r io.Reader, source string) (*Workflow, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", source, err)
	}
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if err := wf.Validate(); err != nil {
		return nil, fmt.Errorf("validating %s: %w", source, err)
	}
	return &wf, nil
}

// projectRoot walks up from dir until it finds a .git directory or reaches
// the filesystem root. Returns the directory containing .git, or dir itself.
func projectRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
