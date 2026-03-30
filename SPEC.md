# ssmx

The CLI that AWS should have built on top of SSM.

## Problem

AWS Systems Manager Session Manager is powerful — agentless-feeling access, no bastion hosts, no SSH keys to manage, cross-fleet command execution. But AWS wrapped it in an API-shaped CLI that forces you to think in instance IDs, document names, and JSON parameters.

The raw `aws ssm start-session --target i-0abc123` experience is bad because:

- You need the instance ID (who remembers those?)
- No file transfer (scp/rsync)
- No intuitive port forwarding
- No SSH config integration
- No way to bookmark or alias connections
- No interactive instance picker
- No help when SSM isn't set up on an instance
- No help when the Session Manager plugin is missing
- SSM documents are powerful but nobody remembers the names
- Running commands across a fleet requires reading API docs

## Solution

`ssmx` makes SSM usable by humans. Search by name, tag, or alias. Smart first-run setup. Intuitive file transfer and port forwarding. Fuzzy-searchable SSM documents. All reversible.

## Commands

```
ssmx                          # interactive picker -> connect
ssmx connect [target]         # get on a box
ssmx run <thing> [targets]    # run commands, scripts, or documents on instances
ssmx forward <target> <ports> # port forwarding
ssmx cp <src> <dst>           # file transfer
ssmx ls                       # list instances + SSM health
ssmx diagnose [target]        # read-only — what's wrong with this instance
ssmx fix [target]             # diagnose + prompt to remediate (reversible)
ssmx config                   # settings, aliases, ssh config generation
```

## Target Resolution

Anywhere a `[target]` is accepted, `ssmx` resolves it in this order:

1. Bookmark alias (from config)
2. Name tag (exact match)
3. Name tag (fuzzy match)
4. Instance ID (`i-*`)

If ambiguous, show an interactive disambiguation picker. Never error when a human could figure it out.

## Detailed Command Behavior

### `ssmx` (no args)

Interactive TUI. Full-screen instance picker with fuzzy search.

Columns: Name tag, instance ID, state, SSM status, private IP.
Filterable by tag, region, profile.

Selecting an instance starts a session. If the instance isn't SSM-reachable, offer to diagnose.

### `ssmx connect [target]`

Start an interactive SSM session.

```
ssmx connect                        # interactive picker
ssmx connect web-prod               # resolve target, connect
ssmx connect web-prod -r us-west-2  # explicit region
ssmx connect web-prod -p staging    # explicit AWS profile
```

### `ssmx run <thing> [targets]`

Run a command, script, or SSM document on one or more instances. Smart resolution determines what `<thing>` is:

1. Quoted string or inline command -> `AWS-RunShellScript`
2. Local file path that exists -> `AWS-RunShellScript` with file contents
3. Known document alias -> resolved SSM document
4. Full SSM document name -> run that document
5. Ambiguous -> show interactive picker

```
ssmx run "df -h" web-prod                          # shell command
ssmx run ./deploy.sh web-prod                       # local script
ssmx run patch web-prod                             # alias -> AWS-PatchInstanceWithRollback
ssmx run AWS-ConfigureAWSPackage web-prod           # full doc name

ssmx run "df -h" --tag env=prod                     # target by tag
ssmx run "systemctl restart app" web-*              # glob on name tags
ssmx run install web-prod --params name=CloudWatch  # doc params
```

Multi-target output streams in the TUI with per-instance panes or merged output with instance-ID prefixes.

When piped (non-TTY), output is clean and scriptable:

```
ssmx run "hostname" --tag env=prod --quiet
```

### `ssmx forward <target> <ports>`

Port forwarding over SSM.

```
ssmx forward web-prod 8080:80                 # local:remote
ssmx forward web-prod --rds my-database:5432  # RDS helper
```

### `ssmx cp <src> <dst>`

File transfer. No native SSM support exists for this — `ssmx` builds it.

```
# Local <-> remote
ssmx cp ./config.yaml web-prod:/etc/app/
ssmx cp web-prod:/var/log/app.log ./

# Remote -> remote (relay through local or S3)
ssmx cp web-prod:/etc/app/config.yaml web-staging:/etc/app/

# Globs and directories
ssmx cp web-prod:/var/log/*.log ./logs/
ssmx cp ./deploy/ web-prod:/opt/app/ --recursive
```

Transfer mechanism:

- Small files (< few MB): base64 encoded via `RunShellScript`
- Large files: temporary S3 intermediary (if available)
- Remote-to-remote: pull to local, push to destination (v1); S3 relay (future)

Progress bar shown in interactive mode.

### `ssmx ls`

List instances and their SSM health.

```
ssmx ls                        # all instances
ssmx ls --tag env=prod         # filter by tag
ssmx ls --unhealthy            # running but SSM-unreachable
ssmx ls --format json          # pipe-friendly
ssmx ls --format tsv
```

Columns: Name, instance ID, state, SSM status (healthy/unhealthy/unknown), private IP, agent version.

### `ssmx diagnose [target]`

Read-only diagnostics. Safe to run anytime, changes nothing.

Checks (in order, short-circuiting where dependency exists):

1. Is the instance running?
2. Is an IAM instance profile attached?
3. Does the role have `AmazonSSMManagedInstanceCore` (or equivalent)?
4. Has the SSM agent ever checked in?
5. Is the agent currently online? (ping status, last ping time, agent version)
6. Network path — VPC endpoints for `ssm`, `ssmmessages`, `ec2messages`, or internet route (only checked if agent hasn't connected despite IAM being correct)

```
$ ssmx diagnose web-prod

  Resolving "web-prod" -> i-0abc123def (us-east-1)

  ok  Instance is running
  err No IAM instance profile attached
  ?   SSM Agent status unknown (agent never checked in)
  ?   Network path unknown

  Likely cause: missing IAM instance profile
  Run `ssmx fix web-prod` to remediate
```

Supports `--format json` for scripting:

```
ssmx ls --unhealthy | xargs -I{} ssmx diagnose {} --format json
```

### `ssmx fix [target]`

Runs diagnose, then offers to fix each issue. Every change is individually approved and reversible.

```
$ ssmx fix web-prod

  Diagnosing web-prod (i-0abc123def)...

  ok  Instance is running
  err Missing policy: AmazonSSMManagedInstanceCore
  err Agent not checking in

  Proposed fixes:

  [1] Attach AmazonSSMManagedInstanceCore to role "web-prod-role"
  [2] Restart instance with SSM Agent install in user data

  Apply fix [1]? (y/n) > y
  ok  Policy attached

  Apply fix [2]? (y/n) > y
  ok  User data updated
  ok  Instance rebooting...
  ok  Agent online (v3.2.1)

  Ready. Run `ssmx connect web-prod`
```

Fixable issues:

| Problem                  | Fix                                            | Auto-fixable |
|--------------------------|------------------------------------------------|--------------|
| No instance profile      | Create role + profile, attach                  | Yes          |
| Wrong IAM policy         | Attach `AmazonSSMManagedInstanceCore`          | Yes          |
| Agent not installed      | Update user data + reboot, or show manual steps| Yes (reboot) |
| Agent stopped            | `send-command` to restart agent service        | Yes          |
| No network path          | Offer to create VPC endpoints                  | Yes (with cost warning) |

### `ssmx fix history` and `ssmx fix undo`

All fixes are recorded locally and reversible.

```
$ ssmx fix history

  ID  Target      Action                           Time
  3   web-prod    Created VPC endpoints (x3)       2 hours ago
  2   web-prod    Updated user data + rebooted     2 hours ago
  1   web-prod    Created role ssmx-SSMRole        2 hours ago

$ ssmx fix undo 3

  This will delete:
  - vpce-0abc (com.amazonaws.us-east-1.ssm)
  - vpce-0def (com.amazonaws.us-east-1.ssmmessages)
  - vpce-0ghi (com.amazonaws.us-east-1.ec2messages)

  Proceed? (y/n) > y
  ok  Deleted 3 VPC endpoints
```

Design rules for undo:

- Only tracks what `ssmx` created. Attaching a policy to an existing role undoes the attachment, not the role.
- Stores previous state (e.g., original user data) for restore, not just deletion.
- Per-action undo, not all-or-nothing.
- Warns when undo will break SSM connectivity.

### `ssmx config`

Manage settings, aliases, bookmarks, and SSH config generation.

```
ssmx config                            # interactive settings
ssmx config alias web-prod i-0abc123   # create alias
ssmx config ssh-gen                    # generate ~/.ssh/config.d/ssmx entries
```

## First Run Experience

On first invocation, `ssmx` checks prerequisites and guides the user through setup:

1. **AWS credentials** — can we call AWS APIs? If not, guide setup.
2. **Session Manager plugin** — installed? If not, detect platform and offer to install:

| Platform              | Method                           |
|-----------------------|----------------------------------|
| macOS (brew)          | `brew install session-manager-plugin` |
| macOS (no brew)       | Download `.pkg` from AWS         |
| Ubuntu/Debian         | Download `.deb`, `dpkg -i`       |
| RHEL/Amazon Linux     | Download `.rpm`, `yum install`   |
| Windows               | Download `.exe` installer        |

3. **Region** — set? If not, prompt.

All of this happens once, transparently, before the user hits a real error.

```
$ ssmx

  First run setup:
  ok  AWS credentials configured (profile: default)
  ok  Region: us-east-1
  err Session Manager plugin missing

  Install now? (y/n) > y
  ok  Installed session-manager-plugin 1.2.x

  Ready. Loading instances...
```

## Output Modes

Every command respects TTY detection:

- **Interactive (TTY)**: bubbletea TUI, progress bars, color, prompts.
- **Piped (non-TTY)**: clean text output, no color, no prompts. Suitable for `jq`, `awk`, `xargs`.

Explicit format flags override: `--format json`, `--format tsv`.

## Document Aliases

Built-in aliases for common SSM documents, user-extensible via config:

```yaml
# ~/.ssmx/config.yaml
doc_aliases:
  patch: AWS-PatchInstanceWithRollback
  install: AWS-ConfigureAWSPackage
  ansible: AWS-RunAnsiblePlaybook
  update-windows: AWS-InstallWindowsUpdates
```

Interactive fuzzy search when the user doesn't remember:

```
ssmx run                    # no thing specified -> fuzzy search documents
```

## Local State

```
~/.ssmx/
  config.yaml               # profiles, aliases, bookmarks, doc aliases, default region
  state.db                  # sqlite — fix history, session history, instance cache
```

- Instance cache with TTL keeps the picker responsive without hitting AWS APIs every time.
- Session history enables frecency-ranked suggestions (most recent + most frequent).
- Fix history stores all mutations with enough detail to reverse them.

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
| SSH proxy       | exec into session-manager-plugin                     |

## Competitive Landscape

Three existing tools occupy this space. All focus narrowly on "connect to an instance without remembering the ID." None address the broader SSM experience.

### aws-gate (Python, 517 stars)

The most established tool. Supports connecting by name tag, IP, DNS, and ASG name. Has config file aliases, SSH ProxyCommand generation, ephemeral key generation via EC2 Instance Connect, port forwarding, and SCP via SSH tunnels.

Gaps: No interactive picker or TUI. No run command support. No diagnostics or remediation. No SSM document support. Python means slower startup and dependency management friction.

https://github.com/xen0l/aws-gate

### gossm (Go, 436 stars)

Closest to ssmx. Has an interactive instance picker, start-session, SSH, SCP, port forwarding, multi-server command execution, and an MFA helper.

Gaps: Basic picker (not a fuzzy search TUI). No diagnostics or remediation. No SSM document support. No aliases or bookmarks. No SSH config generation. Awkward CLI ergonomics (everything through `-e` flag).

https://github.com/gjbae1212/gossm

### sigil (Go, 69 stars)

Lightweight. Supports SSH, SCP, sessions, config profiles (TOML), SSH config integration, and Docker usage.

Gaps: Minimal feature set. No interactive picker. No run command. No diagnostics. Largely unmaintained.

https://github.com/danmx/sigil

### Differentiation

| Capability                     | aws-gate | gossm   | sigil   | ssmx        |
|--------------------------------|----------|---------|---------|-------------|
| Interactive TUI (fuzzy search) | No       | Basic   | No      | Yes         |
| Connect by name/tag/alias      | Yes      | No      | Limited | Yes         |
| Run commands across fleet      | No       | Basic   | No      | Yes         |
| SSM document support           | No       | No      | No      | Yes         |
| File transfer without SSH      | No       | No      | No      | Yes         |
| Diagnose broken SSM setup      | No       | No      | No      | Yes         |
| Auto-fix SSM prerequisites     | No       | No      | No      | Yes         |
| Auto-install session plugin    | No       | No      | No      | Yes         |
| First-run guided setup         | No       | No      | No      | Yes         |
| SSH config generation          | Yes      | No      | Yes     | Yes         |
| Pipe-friendly output           | No       | No      | Limited | Yes         |
| Reversible infrastructure changes | No    | No      | No      | Yes         |

The biggest gaps in the market:

1. **Diagnose + Fix** — no tool addresses why SSM isn't working on an instance or offers to fix it.
2. **SSM documents made discoverable** — nobody exposes the document system. It's one of SSM's most powerful features and completely hidden.
3. **Native file transfer** — all existing tools shell out to SCP through SSH tunnels. ssmx transfers files directly via RunShellScript, no SSH required.
4. **First-run experience** — every tool assumes prerequisites are met. ssmx handles the cold start from zero.
5. **Modern TUI** — gossm has a basic picker but nothing approaching a full bubbletea experience with fuzzy search, streaming output, and interactive prompts.

## Out of Scope (v1)

- Hybrid/multicloud node registration (activations, `mi-*` nodes)
- Custom SSM document authoring
- Daemon/background mode
- Multi-account assume-role chains (respects `AWS_PROFILE` for now)
- Windows target support in `ssmx fix` remediation paths
