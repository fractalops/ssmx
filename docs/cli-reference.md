# ssmx CLI Reference

This document is the canonical command reference for `ssmx`.

For a product overview and examples, start in [README.md](/Users/mfundo/me/ssmx/README.md). For workflow file syntax, see [docs/workflow-schema-reference.md](/Users/mfundo/me/ssmx/docs/workflow-schema-reference.md).

## Invocation model

```bash
ssmx [target] [-- command...]
```

`ssmx` dispatches into different modes depending on flags and whether you pass a positional target and remote command.

## Global flags

These flags are available at the root:

| Flag | Meaning |
| --- | --- |
| `-p, --profile` | AWS profile to use |
| `-r, --region` | AWS region to use |
| `--format` | Output format for modes that support structured output |

Important:

- `--format` is not universal.
- `ssmx` rejects `--format` in command modes that do not produce formatted output instead of silently ignoring it.

## Command modes

### Interactive picker

```bash
ssmx -i
ssmx --interactive
```

Opens the fuzzy-search picker.

Notes:

- searches instances in the current AWS account and region
- `--format` is not supported

### Connect

```bash
ssmx web-prod
ssmx i-0123456789abcdef0
```

Starts an interactive SSM session to the resolved target.

Notes:

- `--format` is not supported
- target resolution uses bookmarks, exact/prefix name matches, then instance ID

### One-shot exec

```bash
ssmx web-prod -- df -h
ssmx web-prod -- "journalctl -u app --since '5 min ago'"
ssmx web-prod -- "sleep 30" --timeout 10s
```

Runs a remote command and streams stdio.

Notes:

- remote exit codes propagate back to the local shell
- stdin is passed through
- `--timeout` applies here
- `--format` is not supported

### List instances

```bash
ssmx -l
ssmx -l --tag env=prod
ssmx -l --unhealthy
ssmx -l --format json
ssmx -l --format tsv
```

Lists instances with SSM status.

Supported flags:

| Flag | Meaning |
| --- | --- |
| `-l, --list` | List instances |
| `--tag key=value` | Filter by EC2 tag; repeatable |
| `--unhealthy` | Show only running instances that are not SSM-online |
| `--format table|json|tsv` | Output format |

Output formats:

- `table`
  - default human-readable output
- `json`
  - machine-readable array of instances
- `tsv`
  - tab-separated table for shell pipelines

### Health checks

```bash
ssmx web-prod --health
ssmx web-prod --health --format json
```

Runs read-only connectivity diagnostics for a target.

Supported flags:

| Flag | Meaning |
| --- | --- |
| `--health` | Run health checks for a target |
| `--format table|json` | Output format |

JSON output includes:

- target information
- summary status
- structured result entries with section, label, severity, and detail

### Port forwarding

```bash
ssmx web-prod -L 8080
ssmx web-prod -L 8080:localhost:8080
ssmx web-prod -L 5432:db.internal:5432
ssmx web-prod -L 5432 --persist
```

Creates one or more SSM-backed port forwards.

Supported flags:

| Flag | Meaning |
| --- | --- |
| `-L, --forward` | Port forward spec; repeatable |
| `--persist` | Auto-reconnect dropped forwards |

Forward forms:

- `8080`
  - local `8080` to instance `localhost:8080`
- `8080:localhost:8080`
  - explicit loopback target on the instance
- `5432:db.internal:5432`
  - tunnel through the instance to a reachable remote host

Notes:

- `--format` is not supported
- a target is required

### Configuration menu

```bash
ssmx --configure
```

Opens the interactive configuration menu.

Current options:

- manage bookmarks
- set default AWS profile
- set default AWS region
- generate SSH config
- show config path

Notes:

- `--format` is not supported

### Workflow list

```bash
ssmx --workflows
```

Lists discovered workflow names.

Notes:

- searches project-local workflows first, then `~/.ssmx/workflows/`
- `--format` is not supported

### Workflow info

```bash
ssmx --workflow-info deploy
ssmx --workflow-info-file /path/to/deploy.yaml
cat deploy.yaml | ssmx --workflow-info -
```

Shows a human-readable view of the workflow schema, inputs, outputs, execution levels, and warnings.

Notes:

- current output is human-readable only
- `--format` is not supported
- risky `always: true` patterns are surfaced as warnings here
- `--workflow-info` and `--workflow-info-file` are mutually exclusive

### Workflow run

Single target:

```bash
ssmx web-prod --run deploy
ssmx web-prod --run deploy --param version=2.1.0
ssmx web-prod --run deploy --dry-run
ssmx web-prod --run deploy --format json
ssmx web-prod --run - --dry-run --format json
ssmx web-prod --run-file /path/to/deploy.yaml
ssmx web-prod --run-file /path/to/deploy.yaml --dry-run --format json
```

Fleet:

```bash
ssmx --run deploy --tag env=prod
ssmx --run deploy --tag env=prod --concurrency 5
ssmx --run deploy --format json
ssmx --run-file /path/to/deploy.yaml --tag env=prod
```

Supported flags:

| Flag | Meaning |
| --- | --- |
| `--run <name>` | Workflow name resolved from discovered directories |
| `--run-file <path>` | Workflow loaded from an explicit file path; use `-` for stdin |
| `--param key=value` | Workflow input; repeatable |
| `--dry-run` | Resolve and plan without executing |
| `--tag key=value` | Fleet targeting override; repeatable |
| `--concurrency` | Fleet concurrency limit; `0` means unlimited |
| `--timeout` | Overall wall-clock timeout |
| `--format table|json` | Output format |

Notes:

- `--run` and `--run-file` are mutually exclusive

Behavior:

- with a positional target, `ssmx` runs a single-instance workflow
- with `--tag` and no positional target, `ssmx` runs fleet mode
- without a positional target or `--tag`, `ssmx` may use workflow `targets:`
- `--tag` overrides workflow `targets:`

Dry-run behavior:

- `--dry-run`
  - prints the resolved step plan in human-readable form
- `--dry-run --format json`
  - emits a structured plan

JSON run behavior:

- single-instance `--format json`
  - emits a `RunSummary`
- fleet `--format json`
  - emits a `FleetRunSummary`
- human-oriented status streaming is suppressed in JSON mode so stdout stays parseable

### SSH proxy

`ssmx --proxy` is an internal command used by generated SSH config.

It is not intended for direct interactive use.

## Target resolution

When a target is accepted, `ssmx` resolves it in this order:

1. bookmark alias from `~/.ssmx/config.yaml`
2. exact EC2 `Name` tag match
3. prefix `Name` tag match
4. instance ID such as `i-...`

Ambiguous matches open an interactive chooser.

## Output format support matrix

| Mode | Supported `--format` values |
| --- | --- |
| `--list` | `table`, `json`, `tsv` |
| `--health` | `table`, `json` |
| `--run` / `--run-file` | `table`, `json` |
| connect / exec / picker / forward / configure / `--workflows` / `--workflow-info` / `--workflow-info-file` | not supported |

## Local state

`ssmx` stores local state under `~/.ssmx/`.

Current files include:

- `config.yaml`
  - bookmarks, default profile, default region, SSH key path, doc aliases
- `state.db`
  - cached instance metadata and other local operational state

## Common recipes

### Resolve a target and open a session

```bash
ssmx web-prod
```

### Run a one-shot command with an exit code

```bash
ssmx web-prod -- systemctl is-active app
```

### Use workflow planning in CI or scripts

```bash
ssmx web-prod --run deploy --dry-run --format json
```

### Check host access before deploy

```bash
ssmx web-prod --health --format json
```
