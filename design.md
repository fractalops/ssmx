# ssmx — Design & Roadmap

## Problem

AWS Systems Manager Session Manager is powerful — agentless access, no bastion hosts, no SSH keys to manage. But AWS wrapped it in an API-shaped CLI that forces you to think in instance IDs, document names, and JSON parameters.

The raw `aws ssm start-session --target i-0abc123` experience fails humans:

- You need the instance ID (who remembers those?)
- No file transfer (scp/rsync)
- No intuitive port forwarding
- No SSH config integration
- No way to bookmark or alias connections
- No interactive instance picker
- No help when SSM isn't set up correctly
- No help when the Session Manager plugin is missing

## Vision

`ssmx` makes SSM usable by humans. Search by name, tag, or alias. Bookmark connections. Diagnose and fix broken setups. Native SSH, SCP, and port forwarding without open ports or bastion hosts.

---

## What's Built

### Core session management

```
ssmx -i                            # interactive instance picker
ssmx web-prod                      # connect by name tag, alias, or instance ID
ssmx web-prod -- df -h             # one-shot command, exit code propagates
ssmx web-prod -- cmd --timeout 30s # one-shot with timeout
```

Target resolution: bookmark alias → exact Name tag → prefix Name tag → instance ID. Ambiguous matches open a picker.

After a session, the instance is auto-bookmarked if new. A prompt offers to rename it; enter keeps the Name tag as the key.

### Port forwarding

```
ssmx web-prod -L 5432:db.internal:5432   # forward to a remote host via instance
ssmx web-prod -L 8080                    # shorthand: local port → instance same port
ssmx web-prod -L 3306:db:3306 --persist  # auto-reconnect on drop
```

No shell opened. Supports multiple `-L` flags per invocation. `--persist` auto-reconnects.

### SSH proxy (EC2 Instance Connect)

`ssmx --configure` generates `~/.ssh/config.d/ssmx` with `ProxyCommand ssmx --proxy %h %r` entries for each bookmark plus a catch-all for raw instance IDs.

Once configured, standard `ssh`, `scp`, `rsync`, and VS Code Remote work against instance names or IDs — no open ports, no long-lived keys. Each connection pushes an ephemeral key via EC2 Instance Connect (60-second TTL).

### Instance listing

```
ssmx -l                      # all instances, SSM health
ssmx -l --tag env=prod       # filter by tag
ssmx -l --unhealthy          # running but SSM-unreachable
ssmx -l --format json        # pipe-friendly
```

Results are cached in SQLite with a 5-minute TTL.

### Connectivity health check

```
ssmx web-prod --health
```

Streams results as each API call returns. Five sections: prerequisites, EC2/SSM instance state, caller IAM simulation, instance role simulation, VPC endpoint check. Three severities: `✓` ok, `!` warning, `✗` error. IAM simulation (`iam:SimulatePrincipalPolicy`) degrades gracefully when denied.

### Settings & config

```
ssmx --configure    # interactive menu: bookmarks, profile, region, SSH config
```

Interactive menu for managing bookmarks (view/rename/remove), setting a default AWS profile, setting a default region, and generating the SSH config.

### First-run experience

On first invocation, checks AWS credentials, session-manager-plugin (installs automatically on macOS and Linux if missing), and region. All transparent, once only.

---

## Roadmap

### Near-term

**`--run`** — run a command, script, or SSM document on one or more instances.

Single-instance exec already works via `ssmx web-prod -- df -h`. `--run` adds fleet targeting and SSM document support:

```
ssmx web-prod --run ./deploy.sh              # local script on one instance
ssmx web-prod --run patch                    # built-in alias → AWS document
ssmx --tag env=prod -- df -h                 # fleet: inline command by tag
ssmx --tag env=prod --run ./deploy.sh        # fleet: script by tag
```

Smart resolution of the `--run` argument: local file path → upload + run via `RunShellScript`, known alias → SSM document, full document name → direct.

Multi-target output: per-instance panes in TTY, prefixed lines when piped.

**`--fix` / `--unfix`** — run `--health`, then offer to remediate each failing check. Every change is individually approved, recorded, and reversible.

```
ssmx web-prod --fix              # diagnose + remediate
ssmx web-prod --unfix            # show fix history, pick a change to reverse
ssmx web-prod --unfix <id>       # reverse a specific fix directly
```

Fixable problems: missing IAM profile, wrong/missing policy, agent not installed, agent stopped, missing VPC endpoints.

**`ssmxcp`** — file transfer without SSH, as a companion binary (mirrors `scp` naming). Spec at `docs/superpowers/specs/2026-04-05-ssmxcp.md`.

```
ssmxcp web-prod:/var/log/app.log ./
ssmxcp ./config.yaml web-prod:/etc/app/
```

Small files via `RunShellScript` + base64; large files via S3 intermediary.

### Later

- `--run` multi-target TUI with live per-instance output panes
- Frecency-ranked instance suggestions (schema exists, not yet populated)
- SSM document fuzzy search (`ssmx web-prod --run` with no argument)
- `--format json` on `--health` for scripting

### Out of scope (v1)

- Hybrid/multicloud node registration (`mi-*` nodes)
- Custom SSM document authoring
- Daemon/background mode
- Multi-account assume-role chains (respects `AWS_PROFILE` for now)
- Windows target support in `--fix` remediation paths

---

## Architecture

```
cmd/                    cobra commands — thin wiring layer
internal/
  aws/                  AWS SDK wrappers (ec2, ssm, iam, sts, ec2instanceconnect)
  session/              session-manager-plugin exec (Connect, Exec, Forward, SSHProxy)
  health/               health check types, IAM simulation, runner
  resolver/             target resolution (alias → name → prefix → ID)
  preflight/            first-run checks, plugin install
  config/               ~/.ssmx/config.yaml (viper)
  state/                ~/.ssmx/state.db (sqlite — cache, session history)
  tui/                  lipgloss styles, bubbletea picker
  ssh/                  key generation, default SSH user detection
```

## Tech Stack

| Concern         | Choice                                              |
|-----------------|-----------------------------------------------------|
| Language        | Go                                                  |
| TUI             | bubbletea + bubbles + lipgloss                      |
| Forms/prompts   | huh                                                 |
| AWS SDK         | aws-sdk-go-v2                                       |
| CLI framework   | cobra                                               |
| Config          | viper + yaml                                        |
| Local state     | sqlite via modernc.org/sqlite (pure Go, no CGO)     |
| SSH proxy       | EC2 Instance Connect + session-manager-plugin       |

## IAM Requirements

See `docs/iam-permissions.md` for the full tiered policy. Summary:

- **Tier 1** (core): `ec2:DescribeInstances`, `ssm:StartSession`, `ssm:TerminateSession`, `ssm:ResumeSession`, `ssm:GetConnectionStatus`, `ssm:DescribeSessions`, `ssm:DescribeInstanceInformation`
- **Tier 2** (SSH proxy): adds `ec2-instance-connect:SendSSHPublicKey`, `ssm:StartSession` on `AWS-StartSSHSession`
- **Tier 3** (`--health`): adds `sts:GetCallerIdentity`, `iam:GetInstanceProfile`, `iam:SimulatePrincipalPolicy`, `ec2:DescribeVpcEndpoints`

---

## Competitive Landscape

| Capability                      | aws-gate | gossm   | sigil   | ssmx    |
|---------------------------------|----------|---------|---------|---------|
| Interactive TUI picker          | No       | Basic   | No      | Yes     |
| Connect by name/tag/alias       | Yes      | No      | Limited | Yes     |
| One-shot command exec           | No       | No      | No      | Yes     |
| Port forwarding                 | Yes      | Yes     | No      | Yes     |
| SSH proxy (EC2 Instance Connect)| Yes      | Yes     | Yes     | Yes     |
| Run commands across fleet       | No       | Basic   | No      | Roadmap |
| File transfer without SSH       | No       | No      | No      | Roadmap |
| Connectivity health check       | No       | No      | No      | Yes     |
| Fix SSM prerequisites (`--fix`)  | No       | No      | No      | Roadmap |
| Auto-install session plugin     | No       | No      | No      | Yes     |
| First-run guided setup          | No       | No      | No      | Yes     |
| SSH config generation           | Yes      | No      | Yes     | Yes     |
| Pipe-friendly output            | No       | No      | Limited | Yes     |
| Reversible infrastructure changes| No      | No      | No      | Roadmap |
