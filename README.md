<div align="center">

# teu

**Find the right pod, VM, or ticket. Open logs, a shell, or the issue. Without leaving the terminal.**

Fuzzy search across all your Kubernetes clusters and SSH inventory, TUI and
unified local web UI, time ranges, pod topology, node info, Grafana / Loki /
Mimir links, VM checks, SSH access, browser shortcuts, and Jira-style issue
providers.

`Go` · `client-go` · `Bubble Tea` · `Cobra`

</div>

---

`teu` is a single binary that keeps you from juggling Kubernetes contexts.
It lists pods across all your clusters in parallel, ranks them by relevance, and
lets you immediately open logs, metrics, k9s, the pod page, or node details.
Add the `vm` tag to search VMs from `~/.ssh/config` instead, run HTTP/SSH
checks, or open a shell. `teu issue OPS-123` opens a ticket, and
`teu issue new` creates one without choosing an implementation flag.

No daemon, no fzf, no copy-pasting pod names, SSH hosts, or ticket keys.

## What `teu` does

| Feature | Detail |
|---|---|
| Multi-cluster search | Queries every kubeconfig context in parallel |
| Fuzzy search | Free-form terms, tag filters, relevance score |
| Time range | `12h 14h`, `-2h`, `from:… to:…` for logs and metrics |
| TUI | List, pod preview, node info, keyboard actions |
| Local web UI | Fast search, pod page, node, events, YAML |
| Actions | k9s, logs, describe, node, browser, clipboard |
| Observability | Grafana, Loki, and Mimir templates per workspace |
| VM inventory | Discovers `~/.ssh/config` hosts and `servers` entries |
| VM checks | HTTP metrics/health, uptime, load, memory, disk, failed units |
| SSH | Copy the SSH command or open an interactive session |
| Pod topology | Ingress, Service, EndpointSlice, Pod, owner chain, PVC, Node |
| Browser shortcuts | `teu link jira` opens configured links by keyword |
| Issue providers | Open/search/create Jira tickets, extensible to other providers |
| Cache | In-memory and on-disk snapshots for repeated searches |

## Preview

```text
$ teu search poddy ns:poddy-system 12h 14h

logs/metrics range: 12:00 → 14:00
SCORE  POD                          NAMESPACE      CONTEXT  ENV  STATUS   AGE  NODE
99%    poddy-postgres-0             poddy-system   default   -    Running  1d   slim
99%    poddy-netmon-b4csk           poddy-system   default   -    Running  15h  slim
99%    poddy-headscale-8659df7bc9   poddy-system   default   -    Running  3h   slim
```

With no arguments, `teu` opens the web UI. With arguments, it opens the TUI
when stdin and stdout are terminals; otherwise it prints a table.

## Installation

Requirements:

- Go 1.25 or newer;
- `kubectl` for `describe`, `logs`, and `node` actions;
- `k9s` is optional;
- Grafana / Loki / Mimir are optional, but required for links.

From source:

```bash
go build -trimpath -o teu ./cmd/teu
install -m 0755 teu ~/.local/bin/teu
```

Or directly:

```bash
go install ./cmd/teu
```

Verify:

```bash
teu version
teu contexts
```

## Quick start

```bash
# Local web UI
teu

# Pod TUI
teu pod poddy

# Name + namespace + time range
teu pod poddy ns:poddy-system 12h 14h

# Free-form tag: prod matches env, context, name, etc.
teu pod api prod -2h

# VMs only
teu vm supervision uat

# Non-interactive output
teu pod api ns:apps 12h 14h --plain
teu search api ns:apps 12h 14h --plain
teu vm supervision uat --table

# Browser shortcuts by keyword
teu link grafana
teu link ticket

# Tickets: open by key, search, or create
teu issue OPS-123
teu issue "checkout timeout"
teu issue new "Pod api crashloop" --from-pod api-123
```

Term order does not matter:

```bash
teu 12h api ns:apps 14h
teu ns:apps api 14h 12h
```

## Search

Free-form terms are fuzzy-matched against pod names, namespaces, contexts,
clusters, environments, owners, nodes, containers, images, and labels.

Filters are exact and combine with free-form terms.

| Filter | Aliases | Example |
|---|---|---|
| `pod:` | `name:` | `pod:api` |
| `namespace:` | `ns:` | `ns:apps` |
| `context:` | `ctx:` | `ctx:prod-*` |
| `cluster:` | | `cluster:eu-west-1` |
| `env:` | `environment:` | `env:prod` |
| `owner:` | `workload:`, `deploy:` | `owner:api` |
| `node:` | | `node:ip-10-0-1-2` |
| `label:` | `labels:`, `lbl:` | `label:app=api` |
| `container:` | | `container:sidecar` |
| `image:` | | `image:nginx` |
| `phase:` | `status:` | `phase:Running` |
| `server:` | `servers:`, `srv:` | `server:sup-uat` |
| `tag:` | `tags:` | `tag:prod` |
| `host:` | `hostname:` | `host:10.0.0.12` |
| `user:` | | `user:deploy` |
| `ssh:` | | `ssh:sup-uat-01` |

Filters support globs (`*`, `?`, `[...]`) and negation with `!` inside the
value:

```bash
teu api 'ns:prod-*' 'phase:!Running'
teu 'label:app=api' 'env:!dev'
```

Multiple `label:`, `container:`, `image:`, and `tag:` filters are combined with
**AND**. For other filter types, multiple values are combined with **OR**.

In VM mode, `name:` is treated as `server:`. Free-form VM terms must all match
at least one field. The `vm`/`vms` selector is removed before matching. Search
fields are the server name, SSH alias, host, user, environment, and tags.

## VMs and SSH

`teu vm [terms...]` searches the VM inventory and skips Kubernetes entirely.
The legacy form `teu supervision uat vm` also works: the `vm` term is a
selector, not a filter.

```bash
teu vm supervision uat      # TUI in a terminal, table otherwise
teu vm supervision uat --table
teu vm supervision uat --check
teu vm supervision uat --json
teu supervision uat vm      # legacy selector form
teu server sup-uat-01       # one server, with details
teu server sup-uat-01 --check --json
teu ssh sup-uat-01          # interactive SSH
teu ssh sup-uat-01 uptime   # run a remote command
```

In the VM TUI: `s` opens SSH, `c` runs checks, `enter` opens the web page,
`y` copies the SSH command, `/` edits the search, `?` shows help.

Inventory sources:

1. every concrete alias in `~/.ssh/config` (wildcards are not listed);
2. `servers` entries in the teu config.

SSH options (`HostName`, `User`, `Port`, `ProxyJump`) are read from
`~/.ssh/config`. Set `ssh_config` to use another file. `Include` files are
followed. Tags are explicit `servers[].tags` plus tokens derived from the name
and SSH alias (`sup-uat-vm-01` → `sup`, `uat`, `vm`, `01`).

### Configure VMs

```bash
teu config server add sup-uat-01 deploy@10.0.0.12 supervision uat vm
teu config server add db01 --ssh db01 --tags db,prod --health-url http://db01:8080/healthz
teu config server list
teu config server remove db01
```

The second positional argument is treated as `user@host` only when it contains
`@`; otherwise it is a tag. Use flags for explicit values:

```bash
teu config server add web01 \
  --host 10.0.0.20 --user ubuntu --port 2222 \
  --env prod --tags web,prod --no-ssh-checks
```

Equivalent YAML:

```yaml
ssh_config: ""              # default: ~/.ssh/config
servers:
  - name: sup-uat-01
    ssh: sup-uat-01
    tags: [supervision, uat, vm]
    metrics_url: "http://sup-uat-01:9100/metrics"
    health_url: "http://sup-uat-01:8080/healthz"
  - name: db01
    host: 10.0.0.12
    user: deploy
    port: 22
    env: prod
    tags: [db, prod]
    ssh_checks: false
```

If `metrics_url` is omitted, `teu` checks
`http://<host>:9100/metrics`. `health_url` is optional. Machine checks use a
read-only SSH script (`uptime`, load, CPU, `free`, `df`, `systemctl --failed`)
with `BatchMode=yes`. Disable them per server with `ssh_checks: false`.

The unified web UI also lists VMs when the query contains `vm`: the same search
box switches to server results. `/servers?q=…` redirects there. Server detail
pages are served at `/server?name=…`; APIs are `/api/servers` and
`/api/server` (add `check=1` to run checks).

## Time ranges

Time tokens restrict Grafana / Loki / Mimir links and log commands. They can
appear anywhere in the query.

| Syntax | Meaning |
|---|---|
| `12h 14h` | Today, from 12:00 to 14:00 |
| `12h30 14h15` | Today, with minutes |
| `12:30 14:30` | Same, using `:` |
| `22h 2h` | From 22:00 to 02:00 the next day |
| `14h 12h` | From 12:00 to 14:00: the shortest window wins |
| `-2h` | Last two hours |
| `last:30m` | Last 30 minutes |
| `-1d`, `-1w` | Last day, last week |
| `from:2026-11-25T12:00 to:2026-11-25T14:00` | Explicit bounds |
| `since:12h until:14h` | Aliases for `from:` / `to:` |
| `today`, `yesterday`, `now` | Relative dates |
| `25/11`, `25/11/2026` | Date only |

Useful rules:

- A bare `2h` means **02:00**, not “two hours ago”.
  Use `-2h` or `last:2h` for a relative range.
- A single absolute token runs from that time until now.
  If the time is in the future, the window is one hour.
- Two times without a date choose the shortest window.
- Without a time token, links use the last hour.
- `kubectl logs` receives `--since-time` for the start of the range.
  kubectl has no `--until` equivalent, so the log stream is not cut off
  automatically at the end of the range.

The `-2h` form is accepted directly on the CLI even though Cobra would normally
treat it as a flag: `teu` normalizes it to `last:2h` before parsing.

## TUI

| Key | Action |
|---|---|
| `↑` `↓` or `j` `k` | Move |
| `g` / `G` | First / last result |
| `/` or `i` | Edit the query |
| `Enter` or `o` | Open the pod page in the web UI |
| `s` | Open k9s on the pod |
| `l` | Follow logs from the start of the range |
| `d` | Paged `kubectl describe pod` |
| `y` | Copy the `kubectl get` command |
| `r` | Refresh all clusters |
| `?` | Help |
| `q` | Quit |

The preview panel shows the pod, then node details: IP, OS, kernel, runtime,
kubelet, CPU/memory/pods, conditions, and taints.

## Web UI

```bash
teu
teu web
teu web --bind 127.0.0.1 --port 8080
teu web --no-open 'api 12h 14h'
```

The UI listens on `127.0.0.1` by default. `--port 0` picks a free port.
Configured browser shortcuts appear above the results, so the same page can
search pods, inspect VMs, and open Grafana / Swagger / Jira links.

It also exposes:

- `/healthz` for health checks;
- `/api/search` for results;
- `/api/pod` for the full pod page;
- `/api/contexts` and `/api/stats`.

The pod page includes a topology graph (Ingress → Service → EndpointSlice →
Pod, owner chain, PVC, Node), containers, conditions, events, volumes, labels,
annotations, YAML, observability links, kubectl commands, and a detailed node
card. Click any topology node for facts and a copyable `kubectl` command.

## Commands

| Command | Purpose |
|---|---|
| `teu pod [terms...]` | Search pods, alias `pods`; TUI in a terminal |
| `teu vm [tags...]` | Search VMs, TUI in a terminal; aliases `servers`, `vms` |
| `teu link [keywords...]` | Open a configured browser shortcut, alias `links` |
| `teu issue [key\|terms...]` | Open, search or create tickets |
| `teu issue new [title...]` | Create a ticket, optional `--from-pod` / `--from-vm` |
| `teu issue providers` | List configured issue providers |
| `teu <provider> ...` | Provider alias, e.g. `teu jira OPS-123` |
| `teu [terms...]` | Legacy root search: pods, or VMs with the `vm` selector |
| `teu` | Local web UI |
| `teu web [terms...]` | Web UI, aliases `ui`, `serve` |
| `teu search [terms...]` | Non-interactive pod search, alias `find` |
| `teu server NAME` | Show one VM, add `--check` or `--json` |
| `teu ssh NAME [command...]` | Open SSH or run a remote command |
| `teu config server add NAME ...` | Add or update a VM |
| `teu config server list` | List discovered VMs/servers |
| `teu config server remove NAME` | Remove a config VM entry |
| `teu config shortcut add NAME URL ...` | Add or update a browser shortcut |
| `teu config shortcut list` | List browser shortcuts |
| `teu config shortcut remove NAME` | Remove a browser shortcut |
| `teu contexts` | List contexts, alias `ctx` |
| `teu config init` | Create an example configuration |
| `teu config path` | Print the effective config path |
| `teu config show` | Print the resolved config |
| `teu config check` | Validate config and kubeconfigs |
| `teu cache dir` | Print the cache directory |
| `teu cache clear` | Delete cached snapshots |
| `teu version` | Print the version |

Main flags:

| Flag | Effect |
|---|---|
| `--config` | Config file path |
| `--context` | Restrict to a context, repeatable, glob supported |
| `--namespace` | Restrict to a namespace, repeatable |
| `--env` | Restrict to an environment |
| `--limit` | Maximum number of results |
| `--concurrency` | Number of clusters queried in parallel |
| `--timeout` | Per-cluster request timeout |
| `--refresh` | Ignore the cache and refresh |
| `--bind` | Web server bind address |
| `--port` | Web server port, `0` = random |
| `--no-open` | Do not open the browser |
| `--json` | Full JSON output for `search` |
| `--plain` | One line per pod for `search` |

## Configuration

Resolution order:

1. `--config <path>`;
2. `$TEU_CONFIG`;
3. `$XDG_CONFIG_HOME/teu/config.yaml`;
4. `~/.config/teu/config.yaml`.

Without a config file, the defaults are enough.

```bash
teu config init
teu config show
teu config check
```

Kubeconfigs:

1. `kube.kubeconfigs` in the config;
2. `$KUBECONFIG`;
3. `~/.kube/config`.

Multiple files are merged through the `client-go` loading rules.

### Minimal example

```yaml
kube:
  kubeconfigs: []
  include_contexts: ["prod-*", "staging-*"]
  exclude_contexts: ["*-readonly"]
  concurrency: 12
  request_timeout: 10s
  cache_ttl: 20s
  all_namespaces: true
  namespaces: []
  exclude_namespaces: []   # e.g. ["kube-system"]
  resolve_owners: false

ui:
  bind: 127.0.0.1
  port: 0
  open_browser: true

ssh_config: ""   # default: ~/.ssh/config
servers: []      # optional tags, metrics/health URLs, explicit SSH settings

shortcuts:
  - name: grafana
    keywords: [grafana, metrics, dashboards]
    url: "https://grafana.example.com"
  - name: jira
    keywords: [jira, ticket, bug]
    url: "https://jira.example.com"

issues:
  default_provider: jira
providers:
  jira:
    type: jira
    base_url: "https://company.atlassian.net"
    username_env: JIRA_USER
    token_env: JIRA_TOKEN
    keywords: [jira, ticket, bug]
    defaults:
      project: OPS
      issue_type: Task

search:
  min_score: 0.35
  limit: 50
  live_limit: 200
```

`resolve_owners` makes extra calls to resolve ReplicaSet → Deployment. Leave it
at `false` if you have many clusters.

### Workspaces

Workspaces map a context pattern to an environment, display name, org ID, and
specific links.

```yaml
workspaces:
  - name: prod
    context: "*-prod-*"
    env: prod
    display: PRODUCTION
    org_id: "1"
    links:
      grafana:
        url: "https://grafana.prod.example.com"
      loki:
        url: "https://loki.prod.example.com"
```

The longest context pattern wins. Workspace links override global links field
by field. If `env` is not set, `teu` infers it from the context name (`prod`,
`staging`, `dev`, `test`, `sandbox`, `local`).

### Links and placeholders

```yaml
links:
  grafana:
    url: "https://grafana.example.com"
    logs_url: "{grafana}/explore?from={from_ms}&to={to_ms}&var-namespace={namespace}&var-pod={pod}"
    pod_url: "{grafana}/d/k8s/pods?from={from_ms}&to={to_ms}&var-pod={pod}"
  loki:
    url: "https://loki.example.com"
    query: '{namespace="{namespace}", pod="{pod}"}'
    explore_url: "{grafana}/explore?from={from_ms}&to={to_ms}&left={namespace}&var-pod={pod}"
  mimir:
    url: "https://mimir.example.com"
    query: 'sum(rate(container_cpu_usage_seconds_total{namespace="{namespace}", pod="{pod}"}[5m]))'
    explore_url: "{grafana}/explore?from={from_ms}&to={to_ms}&left={namespace}&var-pod={pod}"
```

Placeholders available in URLs and queries:

| Placeholder | Value |
|---|---|
| `{context}` | Kubernetes context |
| `{cluster}` | Cluster name |
| `{env}` | Resolved environment |
| `{org_id}` | Workspace org ID |
| `{namespace}` | Pod namespace |
| `{pod}` | Pod name |
| `{container}` | First pod container |
| `{owner}` | Owner or workload |
| `{from}` / `{to}` | RFC3339 bounds |
| `{from_unix}` / `{to_unix}` | Unix seconds |
| `{from_ms}` / `{to_ms}` | Unix milliseconds, useful for Grafana |
| `{from_ns}` / `{to_ns}` | Unix nanoseconds |
| `{grafana}` / `{loki}` / `{mimir}` | Base URLs |

`_q` variants exist for text fields and bounds: `{context_q}`, `{namespace_q}`,
`{pod_q}`, `{from_q}`, `{to_q}`, etc.

Without an explicit time range, `{from}` is now − 1 hour and `{to}` is now.

## Issues and providers

`teu issue` never asks for `--provider`. It detects ticket keys, uses the
configured default provider, or prompts a menu when several providers match.

```bash
teu issue OPS-123              # open a ticket
teu issue "checkout timeout"   # search all configured providers
teu issue new "Pod api crashloop" --from-pod api-123
teu issue new "VM disk full" --from-vm sup-uat-01
teu issue providers
```

Provider aliases work directly and are handy when several providers exist:

```bash
teu jira OPS-123
teu jira search "checkout timeout"
teu jira new "Pod api crashloop"
teu jira                        # recent tickets
```

Jira configuration:

```yaml
issues:
  default_provider: jira
providers:
  jira:
    type: jira
    base_url: "https://company.atlassian.net"
    username_env: JIRA_USER     # Jira Cloud email
    token_env: JIRA_TOKEN       # API token or PAT
    auth_scheme: basic          # basic (Cloud) or bearer (PAT / Server)
    keywords: [jira, ticket, bug]
    defaults:
      project: OPS
      issue_type: Task
```

```bash
export JIRA_USER="me@example.com"
export JIRA_TOKEN="..."
```

Tokens are read from the environment, never from the teu config. Jira uses the
REST API v2 (`/rest/api/2`). Opening a ticket only needs the base URL; search and
create need the API token. Adding another provider means implementing
`internal/providers.TicketProvider`, registering a factory, and adding its
config block. The CLI, TUI, and web UI stay unchanged.

## Browser shortcuts

`teu link` opens a configured URL by keyword. It is separate from the pod
observability links: shortcuts are global browser bookmarks.

```bash
teu link grafana
teu link ticket
teu link --list
teu link --print jira
teu config shortcut add swagger https://api.example.com/swagger api openapi
teu config shortcut add jira https://jira.example.com --keywords jira,ticket,bug
teu config shortcut list
teu config shortcut remove jira
```

```yaml
shortcuts:
  - name: grafana
    keywords: [grafana, metrics, dashboards]
    description: "Grafana home"
    url: "https://grafana.example.com"
  - name: swagger
    keywords: [swagger, api, openapi]
    url: "https://api.example.com/swagger"
  - name: jira
    keywords: [jira, ticket, bug]
    url: "https://jira.example.com"
```

Matching uses the shortcut name, keywords, description, and URL. All arguments
must match. The best match is opened with the default browser; use `--print`
for the URL, `--json` for machine output, or `--list` to inspect candidates.
Shortcuts also appear in the unified web UI for one-click access.

## Cache

- An in-memory snapshot avoids re-running a search during `cache_ttl`.
- An on-disk snapshot is kept in `os.UserCacheDir()/teu`
  (usually `~/.cache/teu`).
- `cache_ttl: 0` disables the cache.
- `teu cache dir` and `teu cache clear` manage the directory.
- The on-disk cache is not written if a cluster returned an error.

## Architecture

```text
cmd/teu              entry point
internal/cli          Cobra, flags, commands
internal/config       YAML config, workspaces, resolution, templates
internal/kube         kubeconfig, contexts, pods, nodes, client-go
internal/search       tag and time parsing, fuzzy engine
internal/app          snapshots, cache, ranking, orchestration
internal/tui          Bubble Tea
internal/servers      SSH config, VM inventory, tag search, HTTP/SSH checks
internal/providers    ticket provider interfaces, registry, Jira adapter
internal/web          HTTP server, templates, assets
internal/actions      kubectl, k9s, logs, browser, clipboard
internal/cache        on-disk cache
```

Pods are read through `v1`; VMs are local inventory only. No CRDs, no
Kubernetes resource mutations, no remote VM provisioning.

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
go build ./...
```

Targeted tests:

```bash
go test ./internal/search   # time and filter parsing
go test ./internal/kube     # nodes and pod detail
go test ./internal/web      # handlers and templates
go test ./internal/tui      # TUI rendering
go test ./internal/config   # config and links
```

Build with version metadata:

```bash
go build -ldflags "-X github.com/SlimaineB/teu/internal/version.Version=v0.1.0" \
  -o teu ./cmd/teu
```

`Commit` and `Date` are also available via `-X` if you want to automate the
version in CI.

## Limitations

- Pod search covers pods only; VM search covers SSH/config inventory only.
- `kubectl logs` has no end bound: `--since-time` applies the start, but the
  stream stays open.
- Node details depend on `get node` RBAC permissions.
- `k9s` must be installed separately; `ssh` must be installed for VM actions.
- The web server is intended for local use.
- Jira support uses REST API v2 and needs network access plus a token for
  search/create. Other providers are not bundled yet.
- The example links and browser shortcuts point to `example.com`: adapt them to
  your Grafana / Loki / Mimir / Jira setup.
- `k8s.io/client-go` is pinned to `v0.35.10` to stay compatible with Go 1.25.

## Security

By default, the web UI listens only on `127.0.0.1`. Do not expose it on a public
interface without authentication or a reverse proxy.

`teu` does not copy your Kubernetes credentials or SSH keys: it uses your
kubeconfigs and SSH agent/config directly. The config created by `config init`
is `0600`. The cache contains pod metadata, not Kubernetes secrets. Machine
checks only run read-only inspection commands. Browser shortcuts only accept
`http` and `https` URLs and open them with the default browser. Issue tokens are
read from environment variables and are never written to the config or logs.
