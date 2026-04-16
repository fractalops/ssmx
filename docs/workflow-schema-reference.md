# ssmx Workflow Schema Reference

This document is the canonical reference for `ssmx` workflow files.

Use it when you want the exact YAML shape, supported step kinds, expression syntax, and current behavioral constraints. For an overview and examples, start in [README.md](/Users/mfundo/me/ssmx/README.md).

## Workflow discovery

`ssmx` resolves workflows in this order:

1. `<project-root>/.ssmx/workflows/<name>.yaml`
2. `~/.ssmx/workflows/<name>.yaml`

Project-local workflows take precedence over personal workflows when names collide.

The project root is discovered by walking upward until `ssmx` finds a `.git/` directory.

You can also load a workflow from stdin:

```bash
cat deploy.yaml | ssmx web-prod --run -
cat deploy.yaml | ssmx --workflow-info -
cat deploy.yaml | ssmx web-prod --run - --dry-run --format json
```

## Top-level schema

```yaml
name: string
description: string
version: string

targets:
  tags:
    key: value
  instance-ids:
    - i-0123456789abcdef0
  max-concurrency: 3

inputs:
  version:
    type: string
    required: true
    default: "1.2.3"
    description: Version to deploy

secrets:
  - name: db_password
    ssm: /app/prod/db_password
    decrypt: true

env:
  APP_ENV: production

steps:
  step-name:
    shell: echo hello

outputs:
  result: "${{ steps.step-name.stdout }}"
```

## Top-level fields

### `name`

Required string. This is the workflow identifier shown by `ssmx --workflows`.

### `description`

Optional string. Shown in human-readable workflow info output when present.

### `version`

Optional string. Stored and surfaced in dry-run and run summaries, but not interpreted semantically by the engine.

### `targets`

Optional default fleet targeting for `ssmx --run`.

```yaml
targets:
  tags:
    env: prod
    role: web
  max-concurrency: 3
```

Supported fields:

- `tags`
  - Map of EC2 tag filters.
  - Combined with AND semantics.
- `instance-ids`
  - Explicit list of instance IDs.
- `max-concurrency`
  - Maximum number of instances to run concurrently in fleet mode.
  - `0` means unlimited.

Constraints:

- `tags` and `instance-ids` are mutually exclusive.
- CLI `--tag` filters override `targets:` when both are present.
- A positional target always routes to single-instance execution.

### `inputs`

Optional map of declared workflow inputs.

```yaml
inputs:
  version:
    type: string
    required: true
    description: Release version
  force:
    type: bool
    default: false
```

Supported fields per input:

- `type`
  - One of `string`, `int`, `bool`
- `required`
  - Optional boolean
- `default`
  - Optional default value
- `description`
  - Optional human-readable description

Behavior:

- Inputs are provided at runtime with `--param key=value`.
- Unknown input keys are rejected.
- Missing required inputs are rejected.
- Optional inputs without defaults resolve to the empty string.
- Defaults are stringified internally when passed into expressions and shell steps.

### `secrets`

Optional list of SSM Parameter Store references.

```yaml
secrets:
  - name: db_password
    ssm: /app/prod/db_password
    decrypt: true
```

Supported fields:

- `name`
  - Local secret reference used as `${{ secrets.<name> }}`
- `ssm`
  - Parameter Store path
- `decrypt`
  - Optional boolean

### `env`

Optional workflow-level environment variables.

```yaml
env:
  APP_ENV: production
  VERSION: "${{ inputs.version }}"
```

These are available to all steps and may contain `${{ }}` expressions.

### `steps`

Required map of named steps.

Each step must set exactly one kind:

- `shell`
- `ssm-doc`
- `workflow`
- `parallel`

### `outputs`

Optional map of workflow-level outputs.

```yaml
outputs:
  deploy_status: "${{ steps.verify.stdout }}"
```

These are returned to parent workflows and surfaced in run summaries when present.

## Step kinds

Each step may use exactly one of the following fields.

### `shell`

Runs a shell command on the target instance.

```yaml
steps:
  deploy:
    shell: |
      aws s3 cp s3://releases/app-${{ inputs.version }}.tar.gz /tmp/
      tar -xzf /tmp/app-${{ inputs.version }}.tar.gz -C /srv/app/
```

Use `shell` for host-side operational work such as deploys, validation, cleanup, or diagnostics.

### `ssm-doc`

Runs an AWS SSM document.

```yaml
steps:
  patch:
    ssm-doc: AWS-RunPatchBaseline
    params:
      Operation: Install
      RebootOption: RebootIfNeeded
```

Notes:

- `params` values are strings in the schema.
- Document names may be full AWS document names or configured `doc_aliases`.

### `workflow`

Invokes another workflow by name.

```yaml
steps:
  bootstrap:
    workflow: install-deps
    with:
      env: "${{ inputs.env }}"
```

Supported workflow-step-only fields:

- `with`
  - Input values passed to the child workflow
- `on-failure`
  - Fallback workflow to invoke if the child workflow step fails

Example:

```yaml
steps:
  deploy:
    workflow: deploy-app
    on-failure:
      workflow: rollback-app
      with:
        version: "${{ inputs.previous_version }}"
```

### `parallel`

Runs a map of named sub-steps concurrently.

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

Constraints inside `parallel` sub-steps:

- nested `parallel` is not supported
- `needs` is not supported
- `if` is not supported
- `always` is not supported
- each sub-step must be exactly one of `shell`, `ssm-doc`, or `workflow`

## Shared step fields

These fields apply to top-level steps regardless of step kind unless otherwise noted.

### `needs`

List of predecessor step names.

```yaml
steps:
  verify:
    shell: curl -fsS http://localhost:8080/healthz
    needs: [deploy, start]
```

Behavior:

- Defines the execution DAG.
- Undefined dependencies are rejected.
- Self-dependencies are rejected.
- Dependency cycles are rejected.

### `if`

Conditional execution expression.

```yaml
steps:
  rollback:
    shell: ./rollback.sh
    if: "${{ inputs.force }}"

  skip-when-flagged:
    shell: ./deploy.sh
    if: "!${{ inputs.skip }}"

  on-env-prod:
    shell: ./notify.sh
    if: "${{ inputs.env }} == prod"

  on-nonzero-exit:
    shell: ./alert.sh
    if: "${{ steps.verify.exitCode }} != 0"
```

Falsy conditions skip the step.

Supported `if:` forms:

| Form | Evaluates to true when |
| --- | --- |
| `${{ inputs.flag }}` | resolved value is `"true"` or `"1"` |
| `!${{ inputs.flag }}` | resolved value is **not** `"true"` or `"1"` |
| `${{ expr }} == value` | resolved left side equals right side (string) |
| `${{ expr }} != value` | resolved left side differs from right side (string) |

Compound expressions (`&&`, `\|\|`, parentheses) are not supported.

### `always`

Boolean flag that allows a step to run even if dependencies failed.

```yaml
steps:
  cleanup:
    shell: rm -f /tmp/deploy.lock
    needs: [deploy]
    always: true
```

This is most appropriate for cleanup and teardown.

Warning:

`always: true` can be dangerous when downstream steps depend on the cleanup step but not on the original failing predecessors. `ssmx` surfaces warnings for this pattern in `--dry-run` and `--workflow-info`.

### `timeout`

Per-step timeout string such as `30s` or `5m`.

### `env`

Step-level environment variables. These override workflow-level `env` keys when names overlap.

### `outputs`

Named outputs captured from the current step.

```yaml
steps:
  verify:
    shell: curl -fsS http://localhost:8080/healthz
    outputs:
      body: "${{ stdout }}"
      code: "${{ exitCode }}"
```

### `params`

Only valid on `ssm-doc` steps.

### `with`

Only valid on `workflow` steps.

### `on-failure`

Only valid on `workflow` steps and must specify a rollback workflow name.

## Expressions

`ssmx` expressions use `${{ ... }}` syntax.

Supported forms:

| Expression | Meaning |
| --- | --- |
| `${{ inputs.name }}` | Declared workflow input |
| `${{ secrets.name }}` | Declared secret value |
| `${{ env.KEY }}` | Workflow or step environment value |
| `${{ steps.NAME.outputs.KEY }}` | Named output from a previous step |
| `${{ steps.NAME.success }}` | Previous step success boolean |
| `${{ steps.NAME.exitCode }}` | Previous step exit code |
| `${{ steps.NAME.stdout }}` | Previous step stdout |
| `${{ target.name }}` | Current target name |
| `${{ target.instance_id }}` | Current instance ID |
| `${{ target.private_ip }}` | Current private IP |
| `${{ stdout }}` | Current step stdout, valid in `outputs:` |
| `${{ exitCode }}` | Current step exit code, valid in `outputs:` |

Notes:

- Expressions are resolved by `ssmx`, not by the remote shell.
- When interpolated into `shell`, values are treated as strings.
- Downstream step references require the referenced step to have executed earlier in the DAG.
- Current-step expressions are limited to `${{ stdout }}` and `${{ exitCode }}`.

## Execution model

`ssmx` computes dependency levels from the workflow DAG:

- steps in the same level may run concurrently
- later levels wait for earlier levels to finish
- parallel blocks run their sub-steps concurrently as a single top-level step

You can inspect this with:

```bash
ssmx --workflow-info deploy
ssmx web-prod --run deploy --dry-run
ssmx web-prod --run deploy --dry-run --format json
```

## Machine-readable surfaces

### Dry-run JSON

```bash
ssmx web-prod --run deploy --dry-run --format json
```

Returns a resolved execution plan including:

- workflow name
- version
- resolved inputs
- step list in execution order
- warnings such as risky `always: true` patterns

### Run summary JSON

```bash
ssmx web-prod --run deploy --format json
ssmx --run deploy --tag env=prod --format json
```

Single-instance runs return a `RunSummary`. Fleet runs return a `FleetRunSummary`.

Both are designed to keep stdout machine-readable and to carry failure details in the JSON payload.

## Validation rules

The loader rejects workflows that violate these structural rules:

- `targets.tags` and `targets.instance-ids` both set
- top-level step with no kind
- top-level step with multiple kinds
- `on-failure` used on a non-`workflow` step
- `on-failure` without a workflow name
- nested `parallel`
- `needs`, `if`, or `always` inside a parallel sub-step
- parallel sub-step with no kind
- parallel sub-step with multiple kinds
- unknown input provided at runtime
- missing required input
- undefined dependency
- self-dependency
- dependency cycle

## Practical guidance

- Prefer `shell` for host-local operational steps.
- Prefer `ssm-doc` when you want a specific AWS-managed document.
- Use `workflow` composition to keep larger workflows readable.
- Keep `always: true` for cleanup and rollback-adjacent work.
- Use `--workflow-info` and `--dry-run` before rolling workflows across fleets.
