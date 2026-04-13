# ssmx

[![Go Report Card](https://goreportcard.com/badge/github.com/fractalops/ssmx)](https://goreportcard.com/report/github.com/fractalops/ssmx)
[![Go Version](https://img.shields.io/github/go-mod/go-version/fractalops/ssmx)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/fractalops/ssmx)](https://github.com/fractalops/ssmx/releases)

Is a utility that aims to simplify aws ssm operations and user experience


## Features

- **Interactive TUI:** fuzzy-search instance picker
- **exec:** `ssmx web-prod -- df -h` streams output live, exit codes propagate
- **Bookmarking:** saves instances you connect
- **interactive setup:** detects missing credentials, region, and Session Manager plugin; offers to install
- **SSH config generation:** `ssmx --configure` → "Generate SSH config" writes ProxyCommand entries to `~/.ssh/config.d/ssmx`
- **Pipe-friendly:** `ssmx -l --format json` and non-TTY output work cleanly in scripts
- **Port forwarding:** Simplified intuitive port forwarding
- **Health diagnostics:** `ssmx <host> --health` streams read-only SSM connectivity checks with pass/warn/error results
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
  config.yaml    # profiles, aliases/bookmarks, default region
  state.db       # sqlite: instance cache
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
