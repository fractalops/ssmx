# ssmx

[![Go Report Card](https://goreportcard.com/badge/github.com/fractalops/ssmx)](https://goreportcard.com/report/github.com/fractalops/ssmx)
[![Go Version](https://img.shields.io/github/go-mod/go-version/fractalops/ssmx)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/fractalops/ssmx)](https://github.com/fractalops/ssmx/releases)

A utility that aims to simplify aws ssm operations and user experience


## Features

- **Interactive TUI:** fuzzy-search instance picker
- **exec:** Execute commands with standard io support
- **Workflow engine:** Multi-step YAML automation with fleet targeting, parallel steps, SSM documents, and sub-workflow composition
- **Bookmarking:** Save aliases instances you connect
- **interactive setup:** Detect missing credentials, region, and Session Manager plugin; offers to install
- **SSH config generation:** Easily Configure SSH over SSM
- **Pipe-friendly:** `ssmx -l --format json` and non-TTY output work cleanly in scripts
- **Port forwarding:** Intuitive port forwarding
- **Health diagnostics:** Run pre-flight checks to diagnose SSM connectivity
- **File copy:** Copy files between, to or from instances over SSM without open ports (separate `ssmcp` binary)

## Installation

**Using curl**
```bash
curl -sSL https://github.com/fractalops/ssmx/releases/latest/download/install.sh | bash
```

**From source**
```bash
git clone https://github.com/fractalops/ssmx.git
cd ssmx
make build
sudo make install-system
```

### Prerequisites

- AWS credentials configured (`aws configure` or environment variables)
- [AWS Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html): ssmx will offer to install it on first run

## Usage

```
ssmx -i                              # fuzzy-search picker across all instances
ssmx web-prod                        # connect by name tag, bookmark, or instance ID
ssmx web-prod -- df -h               # run a one-shot command; exit code propagates
ssmx web-prod -- df -h --timeout 30s # kill remote command after 30 s

ssmx -l                              # list all instances + SSM reachability
ssmx -l --tag env=prod               # filter by EC2 tag (key=value)
ssmx -l --unhealthy                  # show only instances SSM can't reach
ssmx -l --format json                # machine-readable output for scripts

ssmx web-prod -L 8080                # forward local :8080 → instance :8080
ssmx web-prod -L 8080:localhost:8080 # same, explicit form
ssmx web-prod -L 5432:db.internal:5432  # tunnel through instance to a remote host

ssmx web-prod --health               # read-only SSM connectivity diagnostics

ssmx web-prod --run deploy           # run a workflow on a named instance
ssmx --run deploy --tag env=prod     # run a workflow across a fleet of instances

ssmx --configure                     # manage bookmarks, profile, region, SSH config
```

### Target resolution

Anywhere a target is accepted, ssmx resolves it in this order:

1. Bookmark alias (from `~/.ssmx/config.yaml`)
2. Name tag: exact match
3. Name tag: prefix match
4. Instance ID (`i-*`)

Ambiguous matches open an interactive disambiguation picker.

### Exec

```bash
# Run a command and get output back: streams live, no buffering
ssmx web-prod -- "journalctl -u app --since '5 min ago'"

# Pipe stdin through to the remote process
cat deploy.sql | ssmx db-prod -- psql -U app mydb

# Kill after 10 seconds if still running
ssmx web-prod -- "sleep 30" --timeout 10s
```

Exit codes from the remote command propagate as ssmx's own exit code.

### Instance list

```bash
ssmx -l
```

```
  NAME                           INSTANCE ID            STATE     SSM    PRIVATE IP       AGENT
  web-prod                       i-0abc123def456        running   ● online  10.0.1.10     3.2.0.0
  web-staging                    i-0def456abc789        running   ● online  10.0.1.20     3.2.0.0
  worker-prod                    i-0ghi789def012        running   ✕ offline 10.0.2.10
```

### File copy (ssmcp)

```bash
# Upload a file to a remote instance
ssmcp ./deploy.sh web-prod:/tmp/

# Download a log file
ssmcp web-prod:/var/log/app.log ./

# Copy a directory recursively
ssmcp -r ./dist/ web-prod:/srv/app/

# Download config recursively from a remote path
ssmcp -r web-prod:/etc/nginx/ ./nginx-backup/

# Use a specific AWS profile and region
ssmcp --profile staging --region us-west-2 ./script.sh web-staging:/tmp/
```

`ssmcp` uses the same target resolution as `ssmx` (bookmark → Name tag → instance ID) and requires no open ports: it tunnels `sftp` over the SSM SSH session.

### SSH ProxyCommand integration

```bash
ssmx --configure   # select "Generate SSH config"
```

Writes entries to `~/.ssh/config.d/ssmx` so you can `ssh web-prod` directly through SSM: no bastion, no open ports. Add `Include ~/.ssh/config.d/ssmx` to your `~/.ssh/config` to activate.

## Local state

```
~/.ssmx/
 ├── config.yaml  # profiles, aliases/bookmarks, default region
 ├── ssh_key      # ssh keys
 ├── ssh_key.pub 
 └── state.db     # sqlite: instance cache
```

## Workflow Engine

Run multi-step automation workflows on instances over SSM — no agent, no open ports.

### Running workflows

```bash
ssmx web-prod --run deploy               # run workflow on a named instance
ssmx --run deploy --tag env=prod         # fleet mode: run on all tagged instances
ssmx --run deploy --tag env=prod --concurrency 5  # limit to 5 concurrent instances
ssmx web-prod --run deploy --input version=2.1.0  # pass inputs at runtime
ssmx web-prod --run deploy --dry-run     # print steps without executing
ssmx web-prod --run deploy --timeout 10m # hard wall-clock timeout
```

### Workflow files

Place YAML workflow files in `.ssmx/workflows/` relative to where you run `ssmx`, or in `~/.ssmx/workflows/` for global workflows.

```yaml
name: deploy
description: Deploy application to an instance
version: "1.0.0"
inputs:
  version:
    type: string
    required: true
    description: Version to deploy
steps:
  stop-app:
    shell: systemctl stop app
  deploy:
    shell: |
      aws s3 cp s3://releases/app-${{ inputs.version }}.tar.gz /tmp/
      tar -xzf /tmp/app-${{ inputs.version }}.tar.gz -C /srv/app/
    needs: [stop-app]
  start-app:
    shell: systemctl start app
    needs: [deploy]
outputs:
  status: "${{ steps.start-app.stdout }}"
```

### Step kinds

**`shell:`** — run a shell script on the instance

```yaml
steps:
  check:
    shell: |
      echo "Running on ${{ target.instance_id }}"
      uptime
    env:
      DEPLOY_ENV: "${{ inputs.env }}"
    timeout: 5m
    outputs:
      uptime: "${{ stdout }}"
```

**`ssm-doc:`** — run an arbitrary SSM document

```yaml
steps:
  patch:
    ssm-doc: AWS-RunPatchBaseline
    params:
      Operation: Install
      RebootOption: RebootIfNeeded
    outputs:
      summary: "${{ stdout }}"

  install-agent:
    ssm-doc: install          # expands via doc_aliases in ~/.ssmx/config.yaml
    params:
      action: install
      name: AmazonCloudWatchAgent
```

**`workflow:`** — call another workflow as a sub-step

```yaml
steps:
  bootstrap:
    workflow: install-deps
    with:
      env: "${{ inputs.env }}"
  deploy:
    shell: ./deploy.sh
    needs: [bootstrap]
```

**`parallel:`** — run sub-steps concurrently (all run regardless of sibling failure)

```yaml
steps:
  fetch-assets:
    parallel:
      fetch-kernel:
        shell: curl -fsSL ${{ inputs.kernel_url }} -o /tmp/vmlinux
      fetch-rootfs:
        shell: curl -fsSL ${{ inputs.rootfs_url }} -o /tmp/rootfs.squashfs
      install-agent:
        ssm-doc: AWS-ConfigureAWSPackage
        params:
          action: install
          name: AmazonCloudWatchAgent
```

### Step options

| Field | Description |
|---|---|
| `needs:` | List of step names that must complete first |
| `if:` | Expression — skip step if falsy (`${{ inputs.flag }}`) |
| `always:` | Run even if a dependency failed (useful for cleanup) |
| `timeout:` | Per-step timeout (e.g. `30s`, `5m`) |
| `env:` | Environment variables (expressions supported) |
| `outputs:` | Capture step results for downstream steps |
| `on-failure:` | Call a cleanup workflow if this step fails (`workflow: steps` only) |

### Fleet targeting

Workflows can declare default targets. `--tag` always takes priority over `targets:`.

```yaml
# By EC2 tags
targets:
  tags:
    env: prod
    role: web
  max-concurrency: 3   # 0 = unlimited

# By explicit instance IDs
targets:
  instance-ids:
    - i-0abc123def456
    - i-0xyz789abc012
  max-concurrency: 2
```

### Expressions

Steps support `${{ }}` expressions:

| Expression | Value |
|---|---|
| `${{ inputs.name }}` | Workflow input |
| `${{ steps.NAME.stdout }}` | Stdout of a previous step |
| `${{ steps.NAME.exitCode }}` | Exit code of a previous step |
| `${{ steps.NAME.success }}` | Boolean success of a previous step |
| `${{ steps.NAME.outputs.KEY }}` | Named output of a previous step |
| `${{ target.instance_id }}` | Current instance ID |
| `${{ env.VAR }}` | Workflow-level env variable |
| `${{ stdout }}` | Current step stdout (in `outputs:` block only) |
| `${{ exitCode }}` | Current step exit code (in `outputs:` block only) |

### Doc aliases

Define short names for SSM documents in `~/.ssmx/config.yaml`:

```yaml
doc_aliases:
  patch: AWS-RunPatchBaseline
  install: AWS-ConfigureAWSPackage
  run-script: AWS-RunShellScript
```

## Contributing

Pull requests are welcome. For significant changes, open an issue first to discuss what you'd like to change.

```bash
make test      # run tests
make lint      # run linter
make build     # build binary
```

## License

[MIT](LICENSE)
