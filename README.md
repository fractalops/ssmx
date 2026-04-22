# ssmx

[![Go Report Card](https://goreportcard.com/badge/github.com/fractalops/ssmx)](https://goreportcard.com/report/github.com/fractalops/ssmx)
[![Go Version](https://img.shields.io/github/go-mod/go-version/fractalops/ssmx)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/fractalops/ssmx)](https://github.com/fractalops/ssmx/releases)

`ssmx` is an operator- and agent-friendly CLI for AWS Systems Manager.

It makes SSM practical for day-to-day infrastructure work: connect to instances, run commands, forward ports, copy files, diagnose broken access, and automate multi-step workflows across fleets without opening inbound SSH.

## Why ssmx

AWS SSM is powerful, but the default experience is fragmented. Starting sessions, picking the right target, debugging access failures, wiring up SSH-over-SSM, and running repeatable operations across many instances usually takes more glue than it should.

`ssmx` pulls those workflows into one CLI.

It is also designed to be agent-friendly: the core actions are exposed as stable, composable CLI primitives that are easy for automation agents to discover, invoke, and combine.

## What it does

- **Interactive picker:** fuzzy-search instances in your current AWS account and region
- **Smart target resolution:** bookmark alias, exact name, prefix name, or instance ID
- **Exec with stdio:** stream output live, pass stdin through, propagate exit codes
- **Port forwarding:** forward to the instance itself or to a host reachable through it
- **Health checks:** explain why SSM access is failing before you waste time guessing
- **SSH proxying:** generate SSH config and use standard SSH tooling over SSM
- **Workflow engine:** YAML automation with inputs, dependencies, conditionals, parallel steps, sub-workflows, and fleet targeting
- **File transfer:** upload and download files over SSM with the separate `ssmcp` binary
- **Interactive setup:** detect missing region or Session Manager plugin and offer to fix them; surface clear next steps for missing credentials

## Installation

### Using curl

```bash
curl -sSL https://github.com/fractalops/ssmx/releases/latest/download/install.sh | bash
```

### From source

```bash
git clone https://github.com/fractalops/ssmx.git
cd ssmx
make build
sudo make install-system
```

### Prerequisites

- AWS credentials configured with `aws configure` or environment variables
- [AWS Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)

If the plugin is missing, `ssmx` will offer to install it on first run.

For a minimal IAM policy and required permissions, see [docs/iam-permissions.md](/Users/mfundo/me/ssmx/docs/iam-permissions.md).

## Quick start

These are the commands most users care about first:

```bash
ssmx -i                              # fuzzy-search picker across all instances
ssmx web-prod                        # connect by name tag, bookmark, or instance ID
ssmx web-prod -- df -h               # run a one-shot command; exit code propagates
ssmx web-prod -- df -h --timeout 30s # kill remote command after 30s

ssmx -l                              # list all instances + SSM reachability
ssmx -l --tag env=prod               # filter by EC2 tag (key=value)
ssmx -l --unhealthy                  # show only instances SSM can't reach
ssmx -l --format json                # machine-readable output for scripts

ssmx web-prod -L 8080                # forward local :8080 -> instance :8080
ssmx web-prod -L 8080:localhost:8080 # same, explicit form
ssmx web-prod -L 5432:db.internal:5432  # tunnel through instance to a remote host

ssmx web-prod --health               # read-only SSM connectivity diagnostics

ssmx web-prod --run deploy           # run a workflow on a named instance
ssmx --run deploy --tag env=prod     # run a workflow across a fleet of instances

ssmx --configure                     # manage bookmarks, profile, region, SSH config
```

For the full command surface, output modes, and flag behavior, see [docs/cli-reference.md](/Users/mfundo/me/ssmx/docs/cli-reference.md).

## When to use ssmx vs the AWS CLI

Use the AWS CLI when you want low-level AWS primitives.

Use `ssmx` when you want the operator or agent workflow on top:

- better target selection
- interactive session UX
- built-in diagnostics
- SSH tool integration
- reusable workflows over SSM
- composable commands with predictable inputs and outputs
- explicit human and machine-readable command surfaces

## Common tasks

### Connect to an instance

```bash
ssmx web-prod
ssmx i-0123456789abcdef0
ssmx -i
```

### Run a one-shot command

```bash
ssmx web-prod -- "journalctl -u app --since '5 min ago'"
ssmx web-prod -- "sleep 30" --timeout 10s
cat deploy.sql | ssmx db-prod -- psql -U app mydb
```

Remote exit codes propagate back to your shell, so `ssmx` behaves cleanly in scripts.

### List and filter instances

```bash
ssmx -l
ssmx -l --tag env=prod
ssmx -l --unhealthy
ssmx -l --format json
```

### Forward a port

```bash
ssmx web-prod -L 8080
ssmx web-prod -L 8080:localhost:8080
ssmx web-prod -L 5432:db.internal:5432
```

### Diagnose why access is broken

```bash
ssmx web-prod --health
```

This checks common failure points including target resolution, SSM reachability, and IAM-related access issues.

### Copy files over SSM

```bash
ssmcp ./deploy.sh web-prod:/tmp/
ssmcp web-prod:/var/log/app.log ./
ssmcp -r ./dist/ web-prod:/srv/app/
ssmcp -r web-prod:/etc/nginx/ ./nginx-backup/
ssmcp --profile staging --region us-west-2 ./script.sh web-staging:/tmp/
ssmcp --user ubuntu web-prod:/var/log/app.log ./
```

`ssmcp` uses the same target resolution as `ssmx` and tunnels `sftp` over an SSM-backed SSH session.

It currently depends on SSH-over-SSM rather than a native SSM file transfer protocol, so target instances need:

- SSM connectivity
- SSH available on the instance
- EC2 Instance Connect support

### Use SSH tools over SSM

```bash
ssmx --configure
```

Choose `Generate SSH config`, then add this to `~/.ssh/config`:

```sshconfig
Include ~/.ssh/config.d/ssmx
```

After that, standard SSH tools can use SSM transport:

```bash
ssh web-prod
scp app.tar.gz web-prod:/tmp/
rsync -av ./dist/ web-prod:/srv/app/
```

## Target resolution

Anywhere a target is accepted, `ssmx` resolves it in this order:

1. Bookmark alias from `~/.ssmx/config.yaml`
2. Name tag exact match
3. Name tag prefix match
4. Instance ID (`i-*`)

Ambiguous matches open an interactive disambiguation picker.

## Example list output

```bash
ssmx -l
```

```text
  NAME                           INSTANCE ID            STATE     SSM    PRIVATE IP       AGENT
  web-prod                       i-0abc123def456        running   ● online  10.0.1.10     3.2.0.0
  web-staging                    i-0def456abc789        running   ● online  10.0.1.20     3.2.0.0
  worker-prod                    i-0ghi789def012        running   ✕ offline 10.0.2.10
```

## Local state

```text
~/.ssmx/
 ├── config.yaml  # profiles, aliases/bookmarks, default region
 ├── ssh_key      # ssh keys
 ├── ssh_key.pub
 └── state.db     # sqlite: instance cache
```

## Agent Skills

This repo includes local skills for agent-assisted use:

- [`.agents/skills/`](/Users/mfundo/me/ssmx/.agents/skills) for Codex-style discovery
- [`.claude/skills/`](/Users/mfundo/me/ssmx/.claude/skills) for Claude-style discovery

Current repo-local skills:

- `ssmx-cli` for command construction, mode selection, and CLI syntax
- `ssmx-workflows` for workflow authoring, composition, and execution guidance

The skill contents are currently duplicated in both locations for cross-agent compatibility.

## Workflow Engine

The workflow engine is `ssmx`'s lightweight, CLI-driven orchestration layer for AWS SSM. It complements SSM documents rather than replacing them, making it easy to compose shell steps, selected SSM documents, sub-workflows, and fleet targeting in one place.

Use workflows when you want to:

- run the same operational sequence safely every time
- target one instance or an entire fleet
- express dependencies and conditionals in YAML
- mix shell commands, SSM documents, and sub-workflows
- capture outputs for downstream steps
- give an automation agent a lightweight way to plan and execute multi-step ops tasks

### Running workflows

```bash
ssmx web-prod --run deploy                         # run workflow on a named instance
ssmx --run deploy --tag env=prod                   # fleet mode: run on all tagged instances
ssmx --run deploy --tag env=prod --concurrency 5   # limit to 5 concurrent instances
ssmx web-prod --run deploy --param version=2.1.0   # pass inputs at runtime
ssmx web-prod --run deploy --dry-run               # print steps without executing
ssmx web-prod --run deploy --dry-run --format json # machine-readable plan
ssmx web-prod --run deploy --format json           # machine-readable run summary
ssmx web-prod --run deploy --timeout 10m           # hard wall-clock timeout
cat deploy.yaml | ssmx web-prod --run -            # load a workflow from stdin
ssmx web-prod --run-file /path/to/deploy.yaml      # load a workflow from an explicit file path
ssmx --run-file /path/to/deploy.yaml --tag env=prod # --run-file works in fleet mode too
ssmx web-prod --run doc:AWS-RunPatchBaseline       # run a single SSM document as a one-step workflow
ssmx web-prod --run doc:AWS-RunPatchBaseline --param Operation=Install  # with parameters
```

### Workflow files

Place YAML workflow files in the project root's `.ssmx/workflows/` directory, or in `~/.ssmx/workflows/` for global workflows.

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

For the full schema, validation rules, execution model, and expression reference, see [docs/workflow-schema-reference.md](/Users/mfundo/me/ssmx/docs/workflow-schema-reference.md).

### Step kinds

**`shell:`** runs a shell script on the instance.

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

**`ssm-doc:`** runs an arbitrary SSM document.

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
    ssm-doc: install
    params:
      action: install
      name: AmazonCloudWatchAgent
```

`install` expands via `doc_aliases` in `~/.ssmx/config.yaml`.

**`workflow:`** calls another workflow as a sub-step.

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

**`parallel:`** runs sub-steps concurrently.

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

All sub-steps run regardless of sibling failure.

### Step options

| Field | Description |
|---|---|
| `needs:` | List of step names that must complete first |
| `if:` | Expression, skip step if falsy (`${{ inputs.flag }}`) |
| `always:` | Run even if a dependency failed, useful for cleanup |
| `timeout:` | Per-step timeout such as `30s` or `5m` |
| `env:` | Environment variables, expressions supported |
| `outputs:` | Capture step results for downstream steps |
| `on-failure:` | Call a cleanup workflow if this step fails (`workflow:` steps only) |

### Fleet targeting

Workflows can declare default targets. `--tag` always takes priority over `targets:`.

```yaml
# By EC2 tags
targets:
  tags:
    env: prod
    role: web
  max-concurrency: 3

# By explicit instance IDs
targets:
  instance-ids:
    - i-0abc123def456
    - i-0xyz789abc012
  max-concurrency: 2
```

`0` means unlimited concurrency.

### Expressions

Steps support `${{ }}` expressions:

| Expression | Value |
|---|---|
| `${{ inputs.name }}` | Workflow input |
| `${{ steps.NAME.stdout }}` | Stdout of a previous step |
| `${{ steps.NAME.stderr }}` | Stderr of a previous step |
| `${{ steps.NAME.exitCode }}` | Exit code of a previous step |
| `${{ steps.NAME.success }}` | Boolean success of a previous step |
| `${{ steps.NAME.outputs.KEY }}` | Named output of a previous step |
| `${{ target.instance_id }}` | Current instance ID |
| `${{ target.name }}` | Instance Name tag |
| `${{ target.private_ip }}` | Instance private IP address |
| `${{ env.VAR }}` | Workflow-level env variable |
| `${{ stdout }}` | Current step stdout, in `outputs:` only |
| `${{ exitCode }}` | Current step exit code, in `outputs:` only |

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
make test
make lint
make build
```

## License

[MIT](LICENSE)
