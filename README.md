# ssmx

[![Go Report Card](https://goreportcard.com/badge/github.com/fractalops/ssmx)](https://goreportcard.com/report/github.com/fractalops/ssmx)
[![Go Version](https://img.shields.io/github/go-mod/go-version/fractalops/ssmx)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/fractalops/ssmx)](https://github.com/fractalops/ssmx/releases)

**The AWS SSM CLI that AWS should have built.**

AWS Systems Manager Session Manager is powerful — no bastion hosts, no SSH keys, cross-fleet command execution. But the raw `aws ssm start-session --target i-0abc123` experience is painful: you need the instance ID, there's no interactive picker, no aliases, no help when SSM isn't set up, and no intuitive way to run commands or transfer files.

`ssmx` fixes all of that.

## Features

- **Interactive TUI** — fuzzy-search instance picker with SSM health status
- **Smart target resolution** — connect by alias, Name tag, prefix, or instance ID
- **One-shot exec** — `ssmx web-prod -- df -h` streams output live, exit codes propagate
- **Auto-bookmarking** — saves instances you connect to for fast re-access
- **First-run setup** — detects missing credentials, region, and Session Manager plugin; offers to install
- **SSH config generation** — `ssmx config ssh-gen` writes ProxyCommand entries to `~/.ssh/config.d/ssmx`
- **Pipe-friendly** — `ssmx ls --format json` and non-TTY output work cleanly in scripts
- **Platform-aware plugin install** — Homebrew on macOS, `.deb`/`.rpm` on Linux

## Installation

**Using curl**
```bash
curl -sSL https://github.com/fractalops/ssmx/releases/latest/download/install.sh | bash
```

**Homebrew** *(coming soon)*
```bash
brew install fractalops/tap/ssmx
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
- [AWS Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html) — ssmx will offer to install it on first run

## Usage

```
ssmx -i                         # interactive instance picker
ssmx web-prod                   # connect to instance by name
ssmx web-prod -- df -h          # run a one-shot command
ssmx web-prod -- df -h --timeout 30s  # with timeout

ssmx ls                         # list all instances + SSM health
ssmx ls --tag env=prod          # filter by tag
ssmx ls --unhealthy             # show only unreachable instances
ssmx ls --format json           # pipe-friendly output

ssmx config                     # manage bookmarks and settings
ssmx config ssh-gen             # generate SSH ProxyCommand config
```

### Target resolution

Anywhere a target is accepted, ssmx resolves it in this order:

1. Bookmark alias (from `~/.ssmx/config.yaml`)
2. Name tag — exact match
3. Name tag — prefix match
4. Instance ID (`i-*`)

Ambiguous matches open an interactive disambiguation picker.

### One-shot exec

```bash
# Run a command and get output back — streams live, no buffering
ssmx web-prod -- "journalctl -u app --since '5 min ago'"

# Pipe stdin through to the remote process
cat deploy.sql | ssmx db-prod -- psql -U app mydb

# Kill after 10 seconds if still running
ssmx web-prod -- "sleep 30" --timeout 10s
```

Exit codes from the remote command propagate as ssmx's own exit code, so scripts work correctly.

### Instance list

```bash
ssmx ls
```

```
  Name              Instance ID          State    SSM       Private IP
  web-prod          i-0abc123def456      running  ● online  10.0.1.10
  web-staging       i-0def456abc789      running  ● online  10.0.1.20
  worker-prod       i-0ghi789def012      running  ✕ offline 10.0.2.10
```

### SSH ProxyCommand integration

```bash
ssmx config ssh-gen
```

Writes entries to `~/.ssh/config.d/ssmx` so you can `ssh web-prod` directly through SSM — no bastion, no open ports.

## Local state

```
~/.ssmx/
  config.yaml    # profiles, aliases/bookmarks, default region
  state.db       # sqlite — instance cache (5-min TTL), session history
```

## Roadmap

- `ssmx run` — run commands, scripts, or SSM documents across a fleet
- `ssmx forward` — port forwarding over SSM
- `ssmx cp` — file transfer without SSH
- `ssmx diagnose` — read-only SSM health diagnostics
- `ssmx fix` — diagnose + remediate broken SSM setups (reversible)

See [SPEC.md](SPEC.md) for the full design.

## Contributing

Pull requests are welcome. For significant changes, open an issue first to discuss what you'd like to change.

```bash
make test      # run tests
make lint      # run linter
make build     # build binary
```

## License

[MIT](LICENSE)
