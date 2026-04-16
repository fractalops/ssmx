---
name: ssmx-workflows
description: Use this skill when the task involves authoring, reviewing, debugging, composing, or running `ssmx` workflows. Trigger on requests to create YAML workflows, compose sub-workflows, target fleets, design rollout or maintenance flows, use `workflow:` or `parallel:` steps, add inputs/outputs/secrets, reason about `always` and `needs`, or decide how to run or dry-run workflows from the CLI. Prefer this skill whenever the user wants repeatable operational automation over EC2 instances via `ssmx`.
---

# ssmx Workflows

Use this skill for `ssmx` workflow design, composition, and execution guidance.

For exact schema and command behavior, read:

- `/Users/mfundo/me/ssmx/docs/workflow-schema-reference.md`
- `/Users/mfundo/me/ssmx/docs/cli-reference.md`
- `/Users/mfundo/me/ssmx/README.md`

Load the schema reference when you need exact field names, supported expressions, or validation constraints.

## Default approach

When authoring a workflow:

1. Start from the operational goal.
2. Keep each step single-purpose and named by intent.
3. Use `needs` to make ordering explicit.
4. Use `workflow:` composition when a sequence is reusable.
5. Use `parallel:` only for clearly independent work.
6. Add outputs only when downstream steps really need them.
7. Run `--workflow-info` and `--dry-run` before suggesting production execution.

## What the workflow engine is good at

The current engine is strongest for:

- host-level operational procedures
- deploys and rollback-oriented flows
- maintenance and patching
- diagnostics and incident workflows
- fleet-safe orchestration with explicit dependencies

It is weaker as:

- a full control plane
- a continuous reconciliation system
- a cloud resource provisioner

Keep workflows host-oriented and procedural.

## Step selection defaults

Prefer these defaults:

- `shell` for host-local commands and operational logic
- `ssm-doc` when the AWS document is the actual primitive you want
- `workflow` for reusable sequences or cleanup/rollback composition
- `parallel` only when siblings are truly independent

If several approaches could work, default to `shell` unless an AWS-managed document is clearly the better primitive.

## Authoring rules

### Inputs

- Declare every runtime parameter in `inputs:`
- Use `required: true` only when the workflow cannot sensibly default
- Keep input names simple and stable

### Outputs

- Capture only outputs you need downstream
- Prefer named outputs over re-parsing shell text in later steps
- Use workflow-level `outputs:` for values a parent workflow should consume

### Dependencies

- Use `needs:` instead of relying on map order
- Keep the DAG easy to read
- If a step depends on state created by multiple earlier steps, list them all explicitly

### Cleanup and `always`

`always: true` is mainly for cleanup.

Rules:

- prefer `always: true` for teardown or rollback-adjacent steps
- do not make ordinary business-logic steps `always: true`
- if a downstream step depends on an `always: true` step, verify it also depends on the original critical predecessors when needed

The engine warns about risky `always: true` dependency patterns in planning/info surfaces. Treat those warnings as design feedback, not noise.

### Parallel blocks

Use `parallel:` only for sibling work that:

- does not depend on sibling completion
- does not require branching logic inside the block
- is okay to group as one top-level step

Do not try to encode a mini DAG inside `parallel:`.

## Composition guidance

Reach for sub-workflows when:

- the same sequence appears in more than one place
- rollback or cleanup deserves its own workflow
- the parent workflow reads better at a higher level of abstraction

Good reusable sub-workflow candidates:

- host preflight
- deploy artifact
- reload service
- verify app
- gather diagnostics
- rollback

When using `workflow:`:

- pass only declared inputs with `with:`
- keep the child workflow self-contained
- use workflow outputs instead of scraping child internals

## Good workflow shapes

### Safe rollout on one host

1. preflight
2. stop or drain traffic
3. deploy artifact
4. start or reload service
5. verify health
6. cleanup with `always: true`
7. rollback via `on-failure` if appropriate

### Fleet maintenance

1. host preflight
2. maintenance action
3. reboot or restart if needed
4. verify service health
5. run with `--tag` and explicit `--concurrency`

### Diagnostics

1. gather summary facts
2. collect service logs
3. collect resource state
4. capture outputs for downstream reporting

## Running workflows

Use these command patterns:

```bash
ssmx web-prod --run deploy
ssmx web-prod --run deploy --param version=2.1.0
ssmx web-prod --run deploy --dry-run
ssmx web-prod --run deploy --dry-run --format json
ssmx web-prod --run deploy --format json
ssmx --run patch --tag env=prod --concurrency 2
cat deploy.yaml | ssmx web-prod --run -
```

Default execution advice:

- use `--workflow-info` first when reviewing an unfamiliar workflow
- use `--dry-run` before execution
- use `--dry-run --format json` for agents or CI
- use `--format json` for machine-readable run summaries

## Common mistakes to prevent

- Do not invent unsupported fields or step kinds.
- Do not use nested `parallel:`.
- Do not put `needs`, `if`, or `always` inside parallel sub-steps.
- Do not use undeclared inputs in `with:`.
- Do not rely on implicit ordering.
- Do not oversell the engine as replacing AWS SSM documents; it complements them.
- Do not assume current-step expressions beyond `${{ stdout }}` and `${{ exitCode }}`.

## Review checklist

When asked to review a workflow, check:

1. are inputs declared and actually used?
2. is the DAG explicit and easy to follow?
3. are any `always: true` steps risky?
4. are parallel branches truly independent?
5. are outputs minimal and meaningful?
6. can the workflow be dry-run safely?
7. should any block be extracted into a sub-workflow?

## Good answer shape

For authoring requests, prefer:

1. the workflow YAML
2. the exact command to inspect or run it
3. one short note about the main safety caveat or design tradeoff

Keep workflows practical and readable rather than clever.
