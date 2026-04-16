---
name: ssmx-cli
description: Use this skill when the task is to construct, explain, debug, or review `ssmx` CLI commands. Trigger on requests involving target resolution, connecting to instances, running one-shot commands, listing instances, health checks, port forwarding, workflow invocation flags, JSON output modes, or command syntax for humans, agents, scripts, or CI. Prefer this skill whenever the user asks “what command should I run?”, wants machine-readable output, or needs help combining `ssmx` flags correctly.
---

# ssmx CLI

Use this skill for exact `ssmx` command construction and command review.

For full details, read:

- `/Users/mfundo/me/ssmx/docs/cli-reference.md`
- `/Users/mfundo/me/ssmx/README.md`

Load the reference doc when you need exact supported flags or output-mode behavior. Do not guess command syntax from memory if the docs are available locally.

## Core command model

The root shape is:

```bash
ssmx [target] [-- command...]
```

Choose one mode based on intent:

- connect: `ssmx web-prod`
- exec: `ssmx web-prod -- df -h`
- list: `ssmx -l`
- health: `ssmx web-prod --health`
- forward: `ssmx web-prod -L 5432:db.internal:5432`
- configure: `ssmx --configure`
- workflow list: `ssmx --workflows`
- workflow info: `ssmx --workflow-info deploy`
- workflow run: `ssmx web-prod --run deploy`

## Workflow for answering

1. Identify the mode the user actually wants.
2. Prefer the simplest correct command.
3. Include `--profile` or `--region` only when the task needs them.
4. Add `--format json` only for modes that support it.
5. If the command is for automation, prefer machine-readable forms.

## Supported `--format` modes

Do not suggest `--format` universally.

Supported today:

- `ssmx -l --format table|json|tsv`
- `ssmx web-prod --health --format table|json`
- `ssmx ... --run ... --format table|json`

Not supported:

- connect
- exec
- picker
- forward
- configure
- `--workflows`
- `--workflow-info`

If a user wants structured output in an unsupported mode, say so plainly and suggest the closest supported mode.

## Common command patterns

### Connect

```bash
ssmx web-prod
ssmx i-0123456789abcdef0
ssmx -i
```

### One-shot exec

```bash
ssmx web-prod -- systemctl status app
ssmx web-prod -- "journalctl -u app --since '5 min ago'"
ssmx web-prod -- "sleep 30" --timeout 10s
```

Use `--timeout` for bounded automation or safety-sensitive commands.

### List and filter

```bash
ssmx -l
ssmx -l --tag env=prod
ssmx -l --unhealthy
ssmx -l --format json
```

### Health checks

```bash
ssmx web-prod --health
ssmx web-prod --health --format json
```

Recommend health checks before deploy or before debugging broken SSM access.

### Port forwarding

```bash
ssmx web-prod -L 8080
ssmx web-prod -L 8080:localhost:8080
ssmx web-prod -L 5432:db.internal:5432
ssmx web-prod -L 5432 --persist
```

### Workflow commands

```bash
ssmx web-prod --run deploy
ssmx --run deploy --tag env=prod
ssmx web-prod --run deploy --param version=2.1.0
ssmx web-prod --run deploy --dry-run
ssmx web-prod --run deploy --dry-run --format json
ssmx web-prod --run deploy --format json
cat deploy.yaml | ssmx web-prod --run -
```

## Target resolution

When the user supplies a target, remember the resolution order:

1. bookmark alias
2. exact `Name` tag match
3. prefix `Name` tag match
4. instance ID

If the user is likely to hit ambiguity, mention that `ssmx` may open an interactive disambiguation picker.

## Defaults and recommendations

- For human use, prefer concise human-readable commands.
- For CI, agents, or scripts, prefer `--format json` where supported.
- For workflow planning, prefer `--dry-run --format json`.
- For access debugging, prefer `--health --format json`.
- For fleet workflows, prefer explicit `--tag` and `--concurrency` when rollout safety matters.

## Common mistakes to prevent

- Do not put `--format json` on connect/exec/forward/configure commands.
- Do not omit `--` before a remote exec command.
- Do not describe `--workflow-info` as JSON-capable.
- Do not describe instance discovery as multi-region unless the user is explicitly looping regions outside `ssmx`.
- Do not claim `ssmcp` is native file transfer; it currently relies on SSH-over-SSM.

## Good answer shape

When the user asks for a command, give:

1. the exact command
2. one short note if there is an important caveat
3. optionally one machine-readable variant if the task is automation-oriented

Prefer concrete commands over long explanations.
