package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load finds and parses a workflow by name. Project-local
// (.ssmx/workflows/<name>.yaml) takes precedence over personal
// (~/.ssmx/workflows/<name>.yaml).
func Load(name string) (*Workflow, error) {
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
