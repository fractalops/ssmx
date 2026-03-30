# ssmx Phase 1: Foundation + Core Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working `ssmx` CLI with `ssmx ls`, `ssmx connect`, `ssmx` (interactive picker), and `ssmx config` — enough for a developer to replace `aws ssm start-session` entirely in daily use.

**Architecture:** Cobra for CLI structure, with each command in its own file under `cmd/`. All AWS API calls live in `internal/aws/` behind narrow interfaces so they can be tested with fakes. The TUI picker is a self-contained bubbletea model in `internal/tui/`. Config and local SQLite state are in `internal/config/` and `internal/state/` respectively.

**Tech Stack:** Go 1.22+, cobra, bubbletea, bubbles, lipgloss, huh, aws-sdk-go-v2, viper, modernc.org/sqlite

---

## File Map

```
ssmx/
  main.go                           # entry point — hands off to cmd.Execute()
  go.mod
  go.sum

  cmd/
    root.go                         # root cobra command; no-args → TUI picker
    connect.go                      # `ssmx connect [target]`
    ls.go                           # `ssmx ls`
    config.go                       # `ssmx config` + subcommands

  internal/
    aws/
      client.go                     # build aws.Config from profile/region flags
      ec2.go                        # DescribeInstances, DescribeVpcEndpoints, DescribeRouteTables
      ssm.go                        # DescribeInstanceInformation, StartSession
      iam.go                        # GetInstanceProfile, ListAttachedRolePolicies

    resolver/
      resolver.go                   # resolve target string → instance ID
      resolver_test.go

    config/
      config.go                     # load/save ~/.ssmx/config.yaml via viper
      types.go                      # Config, Profile, Alias, DocAlias structs

    state/
      db.go                         # open SQLite, run migrations
      cache.go                      # instance list cache with TTL
      cache_test.go

    tui/
      picker.go                     # bubbletea model: fuzzy instance picker
      styles.go                     # lipgloss colour palette + shared styles

    preflight/
      check.go                      # orchestrate first-run checks
      plugin.go                     # detect + install session-manager-plugin

    session/
      connect.go                    # exec session-manager-plugin subprocess
```

---

## Task 1: Project Scaffold

**Files:**
- Create: `main.go`
- Create: `go.mod`
- Create: `cmd/root.go`

- [ ] **Step 1: Initialise the Go module**

```bash
cd /Users/mfundo/me/ssmx
go mod init github.com/fractalops/ssmx
```

Expected output: `go: creating new go.mod: module github.com/fractalops/ssmx`

- [ ] **Step 2: Install core dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/spf13/viper@latest
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/huh@latest
go get github.com/aws/aws-sdk-go-v2@latest
go get github.com/aws/aws-sdk-go-v2/config@latest
go get github.com/aws/aws-sdk-go-v2/service/ec2@latest
go get github.com/aws/aws-sdk-go-v2/service/ssm@latest
go get github.com/aws/aws-sdk-go-v2/service/iam@latest
go get modernc.org/sqlite@latest
```

- [ ] **Step 3: Write `main.go`**

```go
package main

import "github.com/fractalops/ssmx/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 4: Write `cmd/root.go`**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagProfile string
	flagRegion  string
)

var rootCmd = &cobra.Command{
	Use:   "ssmx",
	Short: "The SSM CLI that AWS should have built",
	Long:  `ssmx makes AWS Systems Manager usable: interactive instance picker, smart target resolution, diagnostics, and more.`,
	// No-args invocation launches the interactive TUI picker.
	// Implemented fully in Task 9 once the TUI and AWS layers exist.
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ssmx: interactive picker coming in Task 9")
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "", "AWS profile to use")
	rootCmd.PersistentFlags().StringVarP(&flagRegion, "region", "r", "", "AWS region to use")
}
```

- [ ] **Step 5: Build and smoke-test**

```bash
go build ./...
./ssmx --help
```

Expected:
```
The SSM CLI that AWS should have built
...
Usage:
  ssmx [flags]
```

- [ ] **Step 6: Commit**

```bash
git init
git add .
git commit -m "feat: project scaffold — cobra root, go.mod, main.go"
```

---

## Task 2: Config (load/save ~/.ssmx/config.yaml)

**Files:**
- Create: `internal/config/types.go`
- Create: `internal/config/config.go`

- [ ] **Step 1: Write `internal/config/types.go`**

```go
package config

// Config is the full contents of ~/.ssmx/config.yaml.
type Config struct {
	DefaultProfile string            `mapstructure:"default_profile" yaml:"default_profile,omitempty"`
	DefaultRegion  string            `mapstructure:"default_region"  yaml:"default_region,omitempty"`
	Aliases        map[string]string `mapstructure:"aliases"         yaml:"aliases,omitempty"`
	DocAliases     map[string]string `mapstructure:"doc_aliases"     yaml:"doc_aliases,omitempty"`
}

// DefaultDocAliases are the built-in SSM document aliases.
var DefaultDocAliases = map[string]string{
	"patch":          "AWS-PatchInstanceWithRollback",
	"install":        "AWS-ConfigureAWSPackage",
	"ansible":        "AWS-RunAnsiblePlaybook",
	"update-windows": "AWS-InstallWindowsUpdates",
}
```

- [ ] **Step 2: Write `internal/config/config.go`**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const configFileName = "config"
const configFileType = "yaml"

// Dir returns the path to the ssmx config directory (~/.ssmx).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".ssmx"), nil
}

// Load reads ~/.ssmx/config.yaml, returning an empty Config if the file does
// not exist yet.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType(configFileType)
	v.AddConfigPath(dir)

	// Set defaults so an empty/missing file still returns a usable struct.
	v.SetDefault("aliases", map[string]string{})
	v.SetDefault("doc_aliases", DefaultDocAliases)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// Config file doesn't exist yet — return defaults.
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to ~/.ssmx/config.yaml, creating the directory if needed.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType(configFileType)
	v.AddConfigPath(dir)

	v.Set("default_profile", cfg.DefaultProfile)
	v.Set("default_region", cfg.DefaultRegion)
	v.Set("aliases", cfg.Aliases)
	v.Set("doc_aliases", cfg.DocAliases)

	path := filepath.Join(dir, configFileName+"."+configFileType)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// SetAlias adds or updates an alias → instance ID mapping and saves.
func SetAlias(name, instanceID string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	cfg.Aliases[name] = instanceID
	return Save(cfg)
}
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/
git commit -m "feat: config load/save via viper (~/.ssmx/config.yaml)"
```

---

## Task 3: SQLite State (open DB + instance cache)

**Files:**
- Create: `internal/state/db.go`
- Create: `internal/state/cache.go`
- Create: `internal/state/cache_test.go`

- [ ] **Step 1: Write `internal/state/db.go`**

```go
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fractalops/ssmx/internal/config"
	_ "modernc.org/sqlite"
)

// Open returns an open SQLite database at ~/.ssmx/state.db, creating it and
// running migrations if it doesn't exist.
func Open() (*sql.DB, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	path := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening state db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating state db: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS instance_cache (
			instance_id   TEXT PRIMARY KEY,
			name          TEXT NOT NULL DEFAULT '',
			state         TEXT NOT NULL DEFAULT '',
			ssm_status    TEXT NOT NULL DEFAULT '',
			private_ip    TEXT NOT NULL DEFAULT '',
			agent_version TEXT NOT NULL DEFAULT '',
			region        TEXT NOT NULL DEFAULT '',
			profile       TEXT NOT NULL DEFAULT '',
			cached_at     INTEGER NOT NULL  -- Unix timestamp
		);

		CREATE TABLE IF NOT EXISTS session_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id TEXT NOT NULL,
			name        TEXT NOT NULL DEFAULT '',
			profile     TEXT NOT NULL DEFAULT '',
			region      TEXT NOT NULL DEFAULT '',
			connected_at INTEGER NOT NULL,  -- Unix timestamp
			count       INTEGER NOT NULL DEFAULT 1
		);

		CREATE TABLE IF NOT EXISTS fix_history (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id  TEXT NOT NULL,
			action_type  TEXT NOT NULL,
			resource_ids TEXT NOT NULL DEFAULT '',  -- JSON array
			prev_state   TEXT NOT NULL DEFAULT '',  -- JSON blob for restore
			created_at   INTEGER NOT NULL           -- Unix timestamp
		);
	`)
	return err
}
```

- [ ] **Step 2: Write `internal/state/cache.go`**

```go
package state

import (
	"database/sql"
	"time"
)

const cacheTTL = 5 * time.Minute

// CachedInstance is a row from the instance_cache table.
type CachedInstance struct {
	InstanceID   string
	Name         string
	State        string
	SSMStatus    string
	PrivateIP    string
	AgentVersion string
	Region       string
	Profile      string
	CachedAt     time.Time
}

// GetCachedInstances returns all cached instances for the given profile+region
// that were cached within the TTL window.
func GetCachedInstances(db *sql.DB, profile, region string) ([]CachedInstance, error) {
	cutoff := time.Now().Add(-cacheTTL).Unix()
	rows, err := db.Query(`
		SELECT instance_id, name, state, ssm_status, private_ip, agent_version, region, profile, cached_at
		FROM instance_cache
		WHERE profile = ? AND region = ? AND cached_at >= ?
		ORDER BY name ASC
	`, profile, region, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []CachedInstance
	for rows.Next() {
		var inst CachedInstance
		var cachedAtUnix int64
		if err := rows.Scan(
			&inst.InstanceID, &inst.Name, &inst.State, &inst.SSMStatus,
			&inst.PrivateIP, &inst.AgentVersion, &inst.Region, &inst.Profile,
			&cachedAtUnix,
		); err != nil {
			return nil, err
		}
		inst.CachedAt = time.Unix(cachedAtUnix, 0)
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// UpsertInstances replaces the cached instance list for a profile+region.
func UpsertInstances(db *sql.DB, instances []CachedInstance) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO instance_cache
			(instance_id, name, state, ssm_status, private_ip, agent_version, region, profile, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			name=excluded.name, state=excluded.state, ssm_status=excluded.ssm_status,
			private_ip=excluded.private_ip, agent_version=excluded.agent_version,
			region=excluded.region, profile=excluded.profile, cached_at=excluded.cached_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, inst := range instances {
		if _, err := stmt.Exec(
			inst.InstanceID, inst.Name, inst.State, inst.SSMStatus,
			inst.PrivateIP, inst.AgentVersion, inst.Region, inst.Profile, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 3: Write `internal/state/cache_test.go`**

```go
package state

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUpsertAndGetCachedInstances(t *testing.T) {
	db := openTestDB(t)

	instances := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
		{InstanceID: "i-002", Name: "worker-01", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.2", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
	}

	if err := UpsertInstances(db, instances); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(got))
	}
	if got[0].Name != "web-prod" {
		t.Errorf("expected web-prod, got %s", got[0].Name)
	}
}

func TestGetCachedInstances_EmptyOnCacheMiss(t *testing.T) {
	db := openTestDB(t)

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 instances on cold cache, got %d", len(got))
	}
}

func TestUpsertInstances_UpdatesExisting(t *testing.T) {
	db := openTestDB(t)

	first := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Update the state.
	second := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "stopped", SSMStatus: "unknown", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(db, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(got))
	}
	if got[0].State != "stopped" {
		t.Errorf("expected state=stopped after update, got %s", got[0].State)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/state/...
```

Expected:
```
ok  	github.com/fractalops/ssmx/internal/state
```

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat: sqlite state — db migrations, instance cache with TTL"
```

---

## Task 4: AWS Client Builder

**Files:**
- Create: `internal/aws/client.go`

- [ ] **Step 1: Write `internal/aws/client.go`**

```go
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// NewConfig builds an aws.Config honouring explicit profile and region
// overrides. Either argument may be empty to fall back to SDK defaults
// (AWS_PROFILE env var, ~/.aws/config, etc.).
func NewConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}

	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}

	// Verify we actually have credentials — fail fast with a human-readable
	// error rather than a cryptic 403 later.
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Config{}, fmt.Errorf("no AWS credentials found (run `aws configure` or set AWS_ACCESS_KEY_ID): %w", err)
	}
	_ = creds

	return cfg, nil
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/aws/client.go
git commit -m "feat: AWS config builder with credential validation"
```

---

## Task 5: EC2 + SSM Instance Listing

**Files:**
- Create: `internal/aws/ec2.go`
- Create: `internal/aws/ssm.go`

These functions are the only place in the codebase that calls AWS. Everything else uses the types they return.

- [ ] **Step 1: Write `internal/aws/ec2.go`**

```go
package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// Instance is a normalised view of an EC2 instance used throughout ssmx.
type Instance struct {
	InstanceID       string
	Name             string // value of the "Name" tag
	State            string // "running", "stopped", etc.
	PrivateIP        string
	PublicIP         string
	Platform         string // "windows" or "" (Linux/other)
	SubnetID         string
	VPCID            string
	IAMProfileARN    string
	// SSM fields populated separately by MergeSSMInfo.
	SSMStatus    string // "online", "offline", "unknown"
	AgentVersion string
	LastPingAt   string
}

// ListInstances returns all EC2 instances visible to the caller, optionally
// filtered by tag key=value pairs (e.g. ["env=prod", "role=web"]).
func ListInstances(ctx context.Context, cfg aws.Config, tagFilters []string) ([]Instance, error) {
	client := ec2.NewFromConfig(cfg)

	input := &ec2.DescribeInstancesInput{}
	if len(tagFilters) > 0 {
		input.Filters = buildTagFilters(tagFilters)
	}

	var instances []Instance
	paginator := ec2.NewDescribeInstancesPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, reservation := range page.Reservations {
			for _, i := range reservation.Instances {
				inst := Instance{
					InstanceID:    aws.ToString(i.InstanceId),
					State:         string(i.State.Name),
					PrivateIP:     aws.ToString(i.PrivateIpAddress),
					PublicIP:      aws.ToString(i.PublicIpAddress),
					SubnetID:      aws.ToString(i.SubnetId),
					VPCID:         aws.ToString(i.VpcId),
					SSMStatus:     "unknown",
				}
				if i.Platform != "" {
					inst.Platform = string(i.Platform)
				}
				if i.IamInstanceProfile != nil {
					inst.IAMProfileARN = aws.ToString(i.IamInstanceProfile.Arn)
				}
				inst.Name = tagValue(i.Tags, "Name")
				instances = append(instances, inst)
			}
		}
	}
	return instances, nil
}

func tagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func buildTagFilters(tagFilters []string) []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(tagFilters))
	for _, tf := range tagFilters {
		parts := strings.SplitN(tf, "=", 2)
		if len(parts) != 2 {
			continue
		}
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + parts[0]),
			Values: []string{parts[1]},
		})
	}
	return filters
}
```

- [ ] **Step 2: Write `internal/aws/ssm.go`**

```go
package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSMInfo holds the Systems Manager view of a managed instance.
type SSMInfo struct {
	InstanceID   string
	PingStatus   string // "Online", "ConnectionLost", "Inactive"
	AgentVersion string
	LastPingAt   string
}

// ListManagedInstances returns SSM's view of all managed instances.
func ListManagedInstances(ctx context.Context, cfg aws.Config) (map[string]SSMInfo, error) {
	client := ssm.NewFromConfig(cfg)

	result := make(map[string]SSMInfo)
	paginator := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, info := range page.InstanceInformationList {
			id := aws.ToString(info.InstanceId)
			result[id] = SSMInfo{
				InstanceID:   id,
				PingStatus:   string(info.PingStatus),
				AgentVersion: aws.ToString(info.AgentVersion),
				LastPingAt:   info.LastPingDateTime.String(),
			}
		}
	}
	return result, nil
}

// MergeSSMInfo enriches a slice of Instance values with SSM ping status and
// agent version from the SSM info map.
func MergeSSMInfo(instances []Instance, ssmInfo map[string]SSMInfo) {
	for i, inst := range instances {
		if info, ok := ssmInfo[inst.InstanceID]; ok {
			switch ssmtypes.PingStatus(info.PingStatus) {
			case ssmtypes.PingStatusOnline:
				instances[i].SSMStatus = "online"
			case ssmtypes.PingStatusConnectionLost:
				instances[i].SSMStatus = "offline"
			default:
				instances[i].SSMStatus = "unknown"
			}
			instances[i].AgentVersion = info.AgentVersion
			instances[i].LastPingAt = info.LastPingAt
		}
	}
}

// StartSession starts an SSM session on the target instance and returns the
// raw response JSON and stream URL needed by session-manager-plugin.
func StartSession(ctx context.Context, cfg aws.Config, instanceID string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	return client.StartSession(ctx, &ssm.StartSessionInput{
		Target: aws.String(instanceID),
	})
}
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/aws/
git commit -m "feat: AWS EC2+SSM list instances with SSM status merge"
```

---

## Task 6: Target Resolver

**Files:**
- Create: `internal/resolver/resolver.go`
- Create: `internal/resolver/resolver_test.go`

The resolver turns a human string ("web-prod", "i-abc123", or a known alias) into an instance ID. It does NOT call AWS itself — it receives the instance list as input so it can be tested cheaply.

- [ ] **Step 1: Write `internal/resolver/resolver.go`**

```go
package resolver

import (
	"fmt"
	"strings"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// ErrAmbiguous is returned when multiple instances match the target.
type ErrAmbiguous struct {
	Target    string
	Matches   []awsclient.Instance
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
```

- [ ] **Step 2: Write `internal/resolver/resolver_test.go`**

```go
package resolver

import (
	"errors"
	"testing"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

var testInstances = []awsclient.Instance{
	{InstanceID: "i-001", Name: "web-prod"},
	{InstanceID: "i-002", Name: "web-staging"},
	{InstanceID: "i-003", Name: "worker-01"},
	{InstanceID: "i-004", Name: ""},
}

func TestResolve_ExactAlias(t *testing.T) {
	aliases := map[string]string{"prod": "i-001"}
	got, err := Resolve("prod", testInstances, aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_ExactNameTag(t *testing.T) {
	got, err := Resolve("web-prod", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_CaseInsensitiveNameTag(t *testing.T) {
	got, err := Resolve("WEB-PROD", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_PrefixMatch(t *testing.T) {
	got, err := Resolve("worker", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-003" {
		t.Errorf("expected i-003, got %s", got.InstanceID)
	}
}

func TestResolve_AmbiguousPrefix(t *testing.T) {
	_, err := Resolve("web", testInstances, nil)
	var ambig *ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
	if len(ambig.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(ambig.Matches))
	}
}

func TestResolve_InstanceID(t *testing.T) {
	got, err := Resolve("i-004", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-004" {
		t.Errorf("expected i-004, got %s", got.InstanceID)
	}
}

func TestResolve_NotFound(t *testing.T) {
	_, err := Resolve("nonexistent", testInstances, nil)
	var notFound *ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/resolver/...
```

Expected:
```
ok  	github.com/fractalops/ssmx/internal/resolver
```

- [ ] **Step 4: Commit**

```bash
git add internal/resolver/
git commit -m "feat: target resolver — alias, name-tag, prefix, instance-id"
```

---

## Task 7: TUI Styles + Instance Picker Model

**Files:**
- Create: `internal/tui/styles.go`
- Create: `internal/tui/picker.go`

- [ ] **Step 1: Write `internal/tui/styles.go`**

```go
package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colour palette.
	colourOnline  = lipgloss.Color("#00d7af") // teal-green
	colourOffline = lipgloss.Color("#ff5f5f") // red
	colourUnknown = lipgloss.Color("#878787") // grey
	colourSelected = lipgloss.Color("#5f87ff") // blue

	// Status indicators.
	StyleOnline  = lipgloss.NewStyle().Foreground(colourOnline).Bold(true)
	StyleOffline = lipgloss.NewStyle().Foreground(colourOffline).Bold(true)
	StyleUnknown = lipgloss.NewStyle().Foreground(colourUnknown)

	// Table chrome.
	StyleHeader   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#ffffff"))
	StyleSelected = lipgloss.NewStyle().Background(colourSelected).Foreground(lipgloss.Color("#ffffff"))
	StyleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

	// Misc.
	StyleBold    = lipgloss.NewStyle().Bold(true)
	StyleSuccess = lipgloss.NewStyle().Foreground(colourOnline)
	StyleError   = lipgloss.NewStyle().Foreground(colourOffline)
	StyleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00"))
)

// SSMStatusStyle returns the appropriate lipgloss style for an SSM status string.
func SSMStatusStyle(status string) lipgloss.Style {
	switch status {
	case "online":
		return StyleOnline
	case "offline":
		return StyleOffline
	default:
		return StyleUnknown
	}
}

// SSMStatusGlyph returns a single character indicator for SSM status.
func SSMStatusGlyph(status string) string {
	switch status {
	case "online":
		return "✓"
	case "offline":
		return "✗"
	default:
		return "?"
	}
}
```

- [ ] **Step 2: Write `internal/tui/picker.go`**

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// PickerResult is returned when the user selects an instance or cancels.
type PickerResult struct {
	Instance *awsclient.Instance // nil if user cancelled (Esc/Ctrl+C)
}

// PickerModel is the bubbletea model for the interactive instance picker.
type PickerModel struct {
	instances []awsclient.Instance
	filtered  []awsclient.Instance
	cursor    int
	search    textinput.Model
	done      bool
	result    PickerResult
	width     int
	height    int
}

// NewPickerModel creates a PickerModel populated with the given instances.
func NewPickerModel(instances []awsclient.Instance) PickerModel {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search..."
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	m := PickerModel{
		instances: instances,
		filtered:  instances,
		search:    ti,
	}
	return m
}

// RunPicker runs the bubbletea instance picker and returns the chosen instance,
// or nil if the user cancelled.
func RunPicker(instances []awsclient.Instance) (*awsclient.Instance, error) {
	m := NewPickerModel(instances)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	result := final.(PickerModel).result
	return result.Instance, nil
}

func (m PickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.done = true
			m.result = PickerResult{Instance: nil}
			return m, tea.Quit

		case "enter":
			if len(m.filtered) > 0 {
				inst := m.filtered[m.cursor]
				m.done = true
				m.result = PickerResult{Instance: &inst}
				return m, tea.Quit
			}

		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		}
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyFilter()
	// Keep cursor in bounds after filter changes.
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	return m, cmd
}

func (m *PickerModel) applyFilter() {
	query := strings.ToLower(m.search.Value())
	if query == "" {
		m.filtered = m.instances
		return
	}
	var out []awsclient.Instance
	for _, inst := range m.instances {
		haystack := strings.ToLower(inst.Name + " " + inst.InstanceID + " " + inst.PrivateIP)
		if strings.Contains(haystack, query) {
			out = append(out, inst)
		}
	}
	m.filtered = out
}

func (m PickerModel) View() string {
	var sb strings.Builder

	// Header.
	sb.WriteString(StyleHeader.Render(" ssmx — select an instance") + "\n\n")

	// Search box.
	sb.WriteString(" " + m.search.View() + "\n\n")

	// Column headers.
	sb.WriteString(StyleDim.Render(fmt.Sprintf(
		"  %-30s %-21s %-9s %-8s %-15s\n",
		"NAME", "INSTANCE ID", "STATE", "SSM", "PRIVATE IP",
	)))

	// Instance rows — cap at visible height.
	maxRows := m.height - 8
	if maxRows < 1 {
		maxRows = 10
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := start; i < end; i++ {
		inst := m.filtered[i]
		ssmGlyph := SSMStatusGlyph(inst.SSMStatus)
		ssmStyle := SSMStatusStyle(inst.SSMStatus)

		name := inst.Name
		if name == "" {
			name = StyleDim.Render("(no name)")
		}

		row := fmt.Sprintf("  %-30s %-21s %-9s %-8s %-15s",
			truncate(name, 30),
			inst.InstanceID,
			inst.State,
			ssmStyle.Render(ssmGlyph),
			inst.PrivateIP,
		)

		if i == m.cursor {
			row = StyleSelected.Render(row)
		}
		sb.WriteString(row + "\n")
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(StyleDim.Render(fmt.Sprintf(
		"  %d instances  ↑↓ navigate  enter select  esc cancel",
		len(m.filtered),
	)))

	return lipgloss.NewStyle().
		Width(m.width).
		Render(sb.String())
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/
git commit -m "feat: bubbletea instance picker with fuzzy search and SSM status"
```

---

## Task 8: Preflight Checks (Session Manager Plugin)

**Files:**
- Create: `internal/preflight/plugin.go`
- Create: `internal/preflight/check.go`

- [ ] **Step 1: Write `internal/preflight/plugin.go`**

```go
package preflight

import (
	"fmt"
	"os/exec"
	"runtime"
)

const pluginBinary = "session-manager-plugin"

// PluginInstalled returns true if session-manager-plugin is on PATH.
func PluginInstalled() bool {
	_, err := exec.LookPath(pluginBinary)
	return err == nil
}

// InstallPlugin attempts to install session-manager-plugin for the current
// platform. It returns an error if installation is not supported automatically.
func InstallPlugin() error {
	switch runtime.GOOS {
	case "darwin":
		return installDarwin()
	case "linux":
		return installLinux()
	default:
		return fmt.Errorf("automatic install not supported on %s — see https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html", runtime.GOOS)
	}
}

func installDarwin() error {
	// Prefer Homebrew if available.
	if _, err := exec.LookPath("brew"); err == nil {
		cmd := exec.Command("brew", "install", "session-manager-plugin")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew install session-manager-plugin: %w", err)
		}
		return nil
	}
	return fmt.Errorf("homebrew not found — install manually: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
}

func installLinux() error {
	// Detect package manager.
	if _, err := exec.LookPath("dpkg"); err == nil {
		return installLinuxDeb()
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		return installLinuxRPM()
	}
	return fmt.Errorf("unsupported Linux distribution — install manually: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
}

func installLinuxDeb() error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.deb",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb"},
		{"sudo", "dpkg", "-i", "/tmp/session-manager-plugin.deb"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}

func installLinuxRPM() error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.rpm",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/linux_64bit/session-manager-plugin.rpm"},
		{"sudo", "yum", "install", "-y", "/tmp/session-manager-plugin.rpm"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}
```

- [ ] **Step 2: Write `internal/preflight/check.go`**

```go
package preflight

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/tui"
)

// Run performs all first-run checks and interactively resolves any failures.
// It returns an error only if a check failure cannot be resolved.
func Run(ctx context.Context, profile, region string) error {
	// 1. AWS credentials.
	if _, err := awsclient.NewConfig(ctx, profile, region); err != nil {
		return fmt.Errorf("%w\n\nRun `aws configure` to set up credentials.", err)
	}
	fmt.Println(tui.StyleSuccess.Render("ok") + "  AWS credentials configured")

	// 2. Region.
	if region == "" {
		cfg, _ := config.Load()
		if cfg != nil {
			region = cfg.DefaultRegion
		}
	}
	if region != "" {
		fmt.Printf("%s  Region: %s\n", tui.StyleSuccess.Render("ok"), region)
	} else {
		fmt.Println(tui.StyleWarning.Render("?") + "  No default region set (use -r or set default_region in ~/.ssmx/config.yaml)")
	}

	// 3. Session Manager plugin.
	if PluginInstalled() {
		fmt.Println(tui.StyleSuccess.Render("ok") + "  Session Manager plugin installed")
		return nil
	}

	fmt.Println(tui.StyleError.Render("err") + " Session Manager plugin not found")

	var install bool
	if err := huh.NewConfirm().
		Title("Install session-manager-plugin now?").
		Value(&install).
		Run(); err != nil || !install {
		return fmt.Errorf("session-manager-plugin is required — install it from https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
	}

	fmt.Print("  Installing... ")
	if err := InstallPlugin(); err != nil {
		fmt.Println()
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Println(tui.StyleSuccess.Render("done"))
	return nil
}
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/preflight/
git commit -m "feat: preflight checks — credential validation + plugin auto-install"
```

---

## Task 9: Session Connect

**Files:**
- Create: `internal/session/connect.go`

- [ ] **Step 1: Write `internal/session/connect.go`**

```go
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// Connect starts an interactive SSM session on instanceID by exec-ing
// session-manager-plugin. The function replaces the current process —
// it only returns if an error occurs before exec.
func Connect(ctx context.Context, cfg aws.Config, instanceID, region, profile string) error {
	// Call SSM to get a session token.
	output, err := awsclient.StartSession(ctx, cfg, instanceID)
	if err != nil {
		return fmt.Errorf("starting SSM session: %w", err)
	}

	// session-manager-plugin expects the StartSession response as JSON.
	responseJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshalling session response: %w", err)
	}

	// Build the parameters argument (just the target).
	paramsJSON, err := json.Marshal(map[string][]string{
		"Target": {instanceID},
	})
	if err != nil {
		return fmt.Errorf("marshalling session params: %w", err)
	}

	// Resolve the SSM endpoint URL for the region.
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)

	// session-manager-plugin argv:
	//   <response-json> <region> StartSession <profile> <params-json> <endpoint>
	pluginPath, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found on PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, pluginPath,
		string(responseJSON),
		region,
		"StartSession",
		profile,
		string(paramsJSON),
		endpoint,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/session/
git commit -m "feat: session connect — exec session-manager-plugin with SSM response"
```

---

## Task 10: `ssmx ls` Command

**Files:**
- Create: `cmd/ls.go`

- [ ] **Step 1: Write `cmd/ls.go`**

```go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/state"
	"github.com/fractalops/ssmx/internal/tui"
)

var (
	lsFlagTags      []string
	lsFlagUnhealthy bool
	lsFlagFormat    string
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List EC2 instances and their SSM health",
	RunE:  runLS,
}

func init() {
	lsCmd.Flags().StringArrayVar(&lsFlagTags, "tag", nil, "Filter by tag (e.g. --tag env=prod)")
	lsCmd.Flags().BoolVar(&lsFlagUnhealthy, "unhealthy", false, "Show only instances with SSM issues")
	lsCmd.Flags().StringVar(&lsFlagFormat, "format", "table", "Output format: table, json, tsv")
	rootCmd.AddCommand(lsCmd)
}

func runLS(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := cfg.Region

	// Check cache first.
	db, err := state.Open()
	if err != nil {
		return err
	}
	defer db.Close()

	profile := flagProfile
	if profile == "" {
		profile = "default"
	}

	cached, err := state.GetCachedInstances(db, profile, region)
	var instances []awsclient.Instance

	if err == nil && len(cached) > 0 {
		// Use cached data.
		for _, c := range cached {
			instances = append(instances, awsclient.Instance{
				InstanceID:   c.InstanceID,
				Name:         c.Name,
				State:        c.State,
				SSMStatus:    c.SSMStatus,
				PrivateIP:    c.PrivateIP,
				AgentVersion: c.AgentVersion,
			})
		}
	} else {
		// Fetch fresh from AWS.
		instances, err = awsclient.ListInstances(ctx, cfg, lsFlagTags)
		if err != nil {
			return fmt.Errorf("listing instances: %w", err)
		}

		ssmInfo, err := awsclient.ListManagedInstances(ctx, cfg)
		if err != nil {
			// Non-fatal: show instances without SSM status.
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not fetch SSM info: %v\n", err)
		} else {
			awsclient.MergeSSMInfo(instances, ssmInfo)
		}

		// Cache the results.
		var toCache []state.CachedInstance
		for _, inst := range instances {
			toCache = append(toCache, state.CachedInstance{
				InstanceID:   inst.InstanceID,
				Name:         inst.Name,
				State:        inst.State,
				SSMStatus:    inst.SSMStatus,
				PrivateIP:    inst.PrivateIP,
				AgentVersion: inst.AgentVersion,
				Region:       region,
				Profile:      profile,
			})
		}
		_ = state.UpsertInstances(db, toCache)
	}

	// Apply --unhealthy filter.
	if lsFlagUnhealthy {
		var filtered []awsclient.Instance
		for _, inst := range instances {
			if inst.State == "running" && inst.SSMStatus != "online" {
				filtered = append(filtered, inst)
			}
		}
		instances = filtered
	}

	return printInstances(instances, lsFlagFormat)
}

func printInstances(instances []awsclient.Instance, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(fmt.Printf).Encode(instances)
	case "tsv":
		fmt.Println(strings.Join([]string{"NAME", "INSTANCE_ID", "STATE", "SSM_STATUS", "PRIVATE_IP", "AGENT_VERSION"}, "\t"))
		for _, inst := range instances {
			fmt.Println(strings.Join([]string{
				inst.Name, inst.InstanceID, inst.State, inst.SSMStatus, inst.PrivateIP, inst.AgentVersion,
			}, "\t"))
		}
		return nil
	default:
		// Table format.
		fmt.Printf("%s\n", tui.StyleHeader.Render(fmt.Sprintf(
			"  %-30s %-21s %-9s %-8s %-15s %-12s",
			"NAME", "INSTANCE ID", "STATE", "SSM", "PRIVATE IP", "AGENT",
		)))
		for _, inst := range instances {
			name := inst.Name
			if name == "" {
				name = tui.StyleDim.Render("(no name)")
			}
			ssmGlyph := tui.SSMStatusGlyph(inst.SSMStatus)
			ssmStyled := tui.SSMStatusStyle(inst.SSMStatus).Render(ssmGlyph)

			fmt.Printf("  %-30s %-21s %-9s %-8s %-15s %-12s\n",
				truncateName(name, 30),
				inst.InstanceID,
				inst.State,
				ssmStyled,
				inst.PrivateIP,
				inst.AgentVersion,
			)
		}
		fmt.Printf("\n%s\n", tui.StyleDim.Render(fmt.Sprintf("  %d instances", len(instances))))
		return nil
	}
}

func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
```

- [ ] **Step 2: Fix the json.Encode call (it uses wrong writer)**

The `json.NewEncoder(fmt.Printf)` in the json case is incorrect. Fix it:

```go
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(instances)
```

Add `"os"` to the imports in `cmd/ls.go`.

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/ls.go
git commit -m "feat: ssmx ls — list instances with SSM health, cache, tag filters, json/tsv output"
```

---

## Task 11: `ssmx connect` Command

**Files:**
- Create: `cmd/connect.go`

- [ ] **Step 1: Write `cmd/connect.go`**

```go
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
)

var connectCmd = &cobra.Command{
	Use:   "connect [target]",
	Short: "Start an interactive SSM session on an instance",
	Long: `Start an interactive SSM session.

With no target, opens an interactive instance picker.
Target can be an alias, Name tag, Name tag prefix, or instance ID.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// First-run checks (idempotent — fast if already set up).
	if err := preflight.Run(ctx, flagProfile, flagRegion); err != nil {
		return err
	}

	awsCfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := awsCfg.Region
	profile := flagProfile
	if profile == "" {
		profile = "default"
	}

	// Load user config for aliases.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var target *awsclient.Instance

	if len(args) == 0 {
		// No target given — show the interactive picker.
		target, err = pickInstance(ctx, awsCfg)
		if err != nil {
			return err
		}
		if target == nil {
			return nil // user cancelled
		}
	} else {
		// Resolve the provided target string.
		instances, err := awsclient.ListInstances(ctx, awsCfg, nil)
		if err != nil {
			return fmt.Errorf("listing instances: %w", err)
		}
		ssmInfo, _ := awsclient.ListManagedInstances(ctx, awsCfg)
		awsclient.MergeSSMInfo(instances, ssmInfo)

		target, err = resolver.Resolve(args[0], instances, cfg.Aliases)
		if err != nil {
			var ambig *resolver.ErrAmbiguous
			if errAs(err, &ambig) {
				// Ambiguous — let the user pick from matching instances.
				fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", args[0], len(ambig.Matches))
				target, err = tui.RunPicker(ambig.Matches)
				if err != nil {
					return err
				}
				if target == nil {
					return nil
				}
			} else {
				return err
			}
		}
	}

	if target.SSMStatus != "online" && target.SSMStatus != "" && target.SSMStatus != "unknown" {
		fmt.Printf("%s  %s (%s) is not reachable via SSM (status: %s)\n",
			tui.StyleWarning.Render("!"),
			target.Name, target.InstanceID, target.SSMStatus,
		)
		fmt.Printf("  Run %s to investigate\n", tui.StyleBold.Render("ssmx diagnose "+target.InstanceID))
		return nil
	}

	fmt.Printf("%s  Connecting to %s (%s)...\n",
		tui.StyleSuccess.Render("→"),
		tui.StyleBold.Render(nameOrID(target)),
		target.InstanceID,
	)

	return session.Connect(ctx, awsCfg, target.InstanceID, region, profile)
}

// pickInstance shows the interactive TUI picker after fetching instances.
func pickInstance(ctx context.Context, cfg awsclient.Config) (*awsclient.Instance, error) {
	instances, err := awsclient.ListInstances(ctx, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	ssmInfo, _ := awsclient.ListManagedInstances(ctx, cfg)
	awsclient.MergeSSMInfo(instances, ssmInfo)
	return tui.RunPicker(instances)
}

func nameOrID(inst *awsclient.Instance) string {
	if inst.Name != "" {
		return inst.Name
	}
	return inst.InstanceID
}

// errAs is a helper to avoid importing errors in multiple places.
func errAs[T error](err error, target *T) bool {
	import "errors"
	return errors.As(err, target)
}
```

- [ ] **Step 2: Fix the errAs helper (inline errors import is invalid)**

Replace the `errAs` helper with a direct `errors.As` call at the use site, and add `"errors"` to imports:

```go
import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
)
```

And replace the ambiguous check:

```go
		var ambig *resolver.ErrAmbiguous
		if errors.As(err, &ambig) {
			fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", args[0], len(ambig.Matches))
			target, err = tui.RunPicker(ambig.Matches)
			if err != nil {
				return err
			}
			if target == nil {
				return nil
			}
		} else {
			return err
		}
```

Remove the `errAs` function entirely.

Also fix the `awsclient.Config` type reference in `pickInstance` — it should be `aws.Config`:

```go
import "github.com/aws/aws-sdk-go-v2/aws"

func pickInstance(ctx context.Context, cfg aws.Config) (*awsclient.Instance, error) {
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/connect.go
git commit -m "feat: ssmx connect — interactive picker or direct target, preflight checks"
```

---

## Task 12: Wire Up `ssmx` (no-args) to the Picker

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Update `rootCmd.RunE` in `cmd/root.go`**

Replace the placeholder `RunE`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	// No-args: delegate to connect with no target (shows picker).
	return runConnect(cmd, args)
},
```

- [ ] **Step 2: Build and smoke-test**

```bash
go build ./...
./ssmx --help
```

- [ ] **Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "feat: ssmx no-args launches interactive instance picker"
```

---

## Task 13: `ssmx config` Command

**Files:**
- Create: `cmd/config.go`

- [ ] **Step 1: Write `cmd/config.go`**

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/tui"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage ssmx settings, aliases, and SSH config",
	RunE:  runConfigInteractive,
}

var configAliasCmd = &cobra.Command{
	Use:   "alias <name> <instance-id>",
	Short: "Create or update an alias for an instance ID",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, id := args[0], args[1]
		if err := config.SetAlias(name, id); err != nil {
			return err
		}
		fmt.Printf("%s  Alias %s → %s saved\n", tui.StyleSuccess.Render("ok"), tui.StyleBold.Render(name), id)
		return nil
	},
}

var configSSHGenCmd = &cobra.Command{
	Use:   "ssh-gen",
	Short: "Generate ~/.ssh/config.d/ssmx with ProxyCommand entries",
	RunE:  runSSHGen,
}

func init() {
	configCmd.AddCommand(configAliasCmd)
	configCmd.AddCommand(configSSHGenCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigInteractive(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var action string
	if err := huh.NewSelect[string]().
		Title("ssmx config").
		Options(
			huh.NewOption("Set default profile", "profile"),
			huh.NewOption("Set default region", "region"),
			huh.NewOption("Add alias", "alias"),
			huh.NewOption("Generate SSH config", "ssh"),
			huh.NewOption("Show config path", "path"),
		).
		Value(&action).
		Run(); err != nil {
		return nil // user cancelled
	}

	switch action {
	case "profile":
		if err := huh.NewInput().
			Title("Default AWS profile").
			Value(&cfg.DefaultProfile).
			Run(); err != nil {
			return nil
		}
		return config.Save(cfg)

	case "region":
		if err := huh.NewInput().
			Title("Default AWS region").
			Placeholder("us-east-1").
			Value(&cfg.DefaultRegion).
			Run(); err != nil {
			return nil
		}
		return config.Save(cfg)

	case "alias":
		var name, id string
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Alias name").Value(&name),
				huh.NewInput().Title("Instance ID").Value(&id),
			),
		).Run(); err != nil {
			return nil
		}
		return config.SetAlias(name, id)

	case "ssh":
		return runSSHGen(cmd, args)

	case "path":
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		fmt.Println(filepath.Join(dir, "config.yaml"))
	}
	return nil
}

const sshConfigTemplate = `# Generated by ssmx — do not edit manually.
# Re-generate with: ssmx config ssh-gen

{{range .Aliases}}
Host {{.Name}}
    IdentityFile ~/.ssh/id_rsa
    User ec2-user
    ProxyCommand ssmx connect --instance-id {{.InstanceID}} --ssh-proxy
{{end}}

# Catch-all for instance IDs.
Host i-* mi-*
    User ec2-user
    ProxyCommand ssmx connect --instance-id %h --ssh-proxy
`

type sshAlias struct {
	Name       string
	InstanceID string
}

func runSSHGen(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Build alias list.
	var aliases []sshAlias
	for name, id := range cfg.Aliases {
		aliases = append(aliases, sshAlias{Name: name, InstanceID: id})
	}

	tmpl, err := template.New("ssh").Parse(sshConfigTemplate)
	if err != nil {
		return err
	}

	// Write to ~/.ssh/config.d/ssmx.
	sshDir := filepath.Join(os.Getenv("HOME"), ".ssh", "config.d")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", sshDir, err)
	}
	outPath := filepath.Join(sshDir, "ssmx")

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating SSH config: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, map[string]interface{}{"Aliases": aliases}); err != nil {
		return err
	}

	fmt.Printf("%s  Written to %s\n", tui.StyleSuccess.Render("ok"), outPath)
	fmt.Println()
	fmt.Println(tui.StyleDim.Render("  Add this to your ~/.ssh/config:"))
	fmt.Printf("    %s\n", tui.StyleBold.Render("Include ~/.ssh/config.d/ssmx"))
	_ = strings.TrimSpace // silence unused import
	return nil
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add cmd/config.go
git commit -m "feat: ssmx config — interactive settings, alias management, SSH config generation"
```

---

## Task 14: End-to-End Smoke Test

This task verifies the full binary works correctly against the real help output before wrapping up Phase 1.

- [ ] **Step 1: Build a release binary**

```bash
go build -o ./ssmx .
```

- [ ] **Step 2: Verify root help**

```bash
./ssmx --help
```

Expected output includes: `connect`, `ls`, `config`, `forward`, flags `-p`, `-r`.

- [ ] **Step 3: Verify subcommand help**

```bash
./ssmx connect --help
./ssmx ls --help
./ssmx config --help
```

Each should show usage, flags, and subcommands (config alias, config ssh-gen).

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected:
```
ok  	github.com/fractalops/ssmx/internal/resolver
ok  	github.com/fractalops/ssmx/internal/state
```

- [ ] **Step 5: Run go vet**

```bash
go vet ./...
```

Expected: no output (no issues).

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "build: phase 1 complete — ls, connect, config, TUI picker, preflight checks"
```

---

## Spec Coverage Checklist

| Spec requirement | Task |
|---|---|
| `ssmx` no-args → interactive picker | Task 7, 12 |
| `ssmx connect [target]` | Task 11 |
| `ssmx ls` with tag filter, unhealthy, json/tsv | Task 10 |
| `ssmx config alias` | Task 13 |
| `ssmx config ssh-gen` | Task 13 |
| `ssmx config` interactive | Task 13 |
| Target resolution: alias, name tag, prefix, instance ID | Task 6 |
| Ambiguous target → disambiguation picker | Task 11 |
| First-run: credential check | Task 8 |
| First-run: session-manager-plugin detection + install | Task 8 |
| Instance cache with TTL | Task 3 |
| AWS config with profile/region flags | Task 4 |
| SSM status merged into instance list | Task 5 |
| TUI: fuzzy search, status glyphs, styles | Task 7 |
| Session connect via session-manager-plugin | Task 9 |

**Phase 2 (separate plan):** `ssmx run`, `ssmx forward`, `ssmx cp`, `ssmx diagnose`, `ssmx fix` with history/undo.
