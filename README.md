<div align="center">

# hop

**Find the right pod, VM, repository, package, or ticket. Open it without leaving the terminal.**

A modular cockpit over Kubernetes clusters, SSH inventory, local Git
repositories, container registries, browser shortcuts, and Jira-style tickets.
Unified TUI and local web UI, live watch, time ranges, pod topology, node info,
Grafana / Loki / Mimir links, VM checks, SSH access, and opt-in command plugins.

`Go` · `client-go` · `Bubble Tea` · `Cobra`

</div>

---

`hop` is a single binary that keeps you from juggling Kubernetes contexts.
It lists pods across all your clusters in parallel, ranks them by relevance, and
lets you immediately open logs, metrics, k9s, the pod page, or node details.
Add the `vm` tag to search VMs from `~/.ssh/config` instead, run HTTP/SSH
checks, or open a shell. `hop issue OPS-123` opens a ticket, and
`hop issue new` creates one without choosing an implementation flag.

No daemon, no fzf, no copy-pasting pod names, SSH hosts, or ticket keys.

## What `hop` does

| Feature | Detail |
|---|---|
| Multi-cluster search | Queries every kubeconfig context in parallel |
| Fuzzy search | Free-form terms, tag filters, relevance score |
| Time range | `12h 14h`, `-2h`, `from:… to:…` for logs and metrics |
| TUI | List, pod preview, node info, keyboard actions |
| Kubernetes dashboard | Fleet KPIs, env/app/cluster/namespace health, nodes, triage |
| Server dashboard | Inventory, HTTP/SSH checks, memory, disk, failed units |
| Local web UI | Fast search, pod page, node, events, YAML |
| Actions | k9s, logs, describe, node, browser, clipboard |
| Observability | Grafana, Loki, and Mimir templates per workspace |
| VM inventory | Discovers `~/.ssh/config` hosts and `servers` entries |
| VM checks | HTTP metrics/health, uptime, load, memory, disk, failed units |
| SSH | Copy the SSH command or open an interactive session |
| Pod topology | Ingress, Service, EndpointSlice, Pod, owner chain, PVC, Node |
| Browser shortcuts | `hop link jira` opens configured links by keyword |
| Issue providers | Open/search/create Jira tickets, extensible to other providers |
| Cockpit search | One query across pods, VMs, repos, packages, tickets, shortcuts |
| Git repositories | Discovers repos, branches, commits, dirty state, ahead/behind |
| Git links | GitHub / GitLab repo, branch, commit, MR and new-MR links |
| Packages | GHCR, GitLab and Docker registry images with live tags |
| Live watch | Polls local snapshots while the TUI is open |
| Plugins | External command sources with a JSON contract, toggleable per source |
| Cache | In-memory and on-disk snapshots for repeated searches |

## Preview

```text
$ hop search poddy ns:poddy-system 12h 14h

logs/metrics range: 12:00 → 14:00
SCORE  POD                          NAMESPACE      CONTEXT  ENV  STATUS   AGE  NODE
99%    poddy-postgres-0             poddy-system   default   -    Running  1d   slim
99%    poddy-netmon-b4csk           poddy-system   default   -    Running  15h  slim
99%    poddy-headscale-8659df7bc9   poddy-system   default   -    Running  3h   slim
```

With no arguments, `hop` opens the infrastructure dashboard in the web UI.
With arguments, it opens the TUI when stdin and stdout are terminals;
otherwise it prints a table.

## Installation

Requirements:

- Go 1.25 or newer;
- `kubectl` for `describe`, `logs`, and `node` actions;
- `k9s` is optional;
- Grafana / Loki / Mimir are optional, but required for links.

From source:

```bash
go build -trimpath -o hop ./cmd/hop
install -m 0755 hop ~/.local/bin/hop
```

Or directly:

```bash
go install ./cmd/hop
```

Verify:

```bash
hop version
hop contexts
```

## Quick start

```bash
# Unified cockpit TUI (live watch on)
hop all

# Focus one source with a selector
hop all git hop
hop all pkg nginx
hop all vm supervision

# Infrastructure dashboard
hop
hop web

# Pod TUI
hop pod poddy

# Name + namespace + time range
hop pod poddy ns:poddy-system 12h 14h

# Free-form tag: prod matches env, context, name, etc.
hop pod api prod -2h

# VMs only
hop vm supervision uat

# Non-interactive output
hop pod api ns:apps 12h 14h --plain
hop search api ns:apps 12h 14h --plain
hop vm supervision uat --table

# Repositories and packages
hop git hop
hop git --links
hop pkg nginx

# Sources: inspect and toggle without deleting config
hop config sources
hop config disable tickets
hop config enable git
hop config sources --json

# Browser shortcuts by keyword
hop link grafana
hop link ticket

# Tickets: open by key, search, or create
hop issue OPS-123
hop issue "checkout timeout"
hop issue new "Pod api crashloop" --from-pod api-123
```

Term order does not matter:

```bash
hop 12h api ns:apps 14h
hop ns:apps api 14h 12h
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
hop api 'ns:prod-*' 'phase:!Running'
hop 'label:app=api' 'env:!dev'
```

Multiple `label:`, `container:`, `image:`, and `tag:` filters are combined with
**AND**. For other filter types, multiple values are combined with **OR**.

In VM mode, `name:` is treated as `server:`. Free-form VM terms must all match
at least one field. The `vm`/`vms` selector is removed before matching. Search
fields are the server name, SSH alias, host, user, environment, and tags.

## VMs and SSH

`hop vm [terms...]` searches the VM inventory and skips Kubernetes entirely.
The legacy form `hop supervision uat vm` also works: the `vm` term is a
selector, not a filter.

```bash
hop vm supervision uat      # TUI in a terminal, table otherwise
hop vm supervision uat --table
hop vm supervision uat --check
hop vm supervision uat --json
hop supervision uat vm      # legacy selector form
hop server sup-uat-01       # one server, with details
hop server sup-uat-01 --check --json
hop ssh sup-uat-01          # interactive SSH
hop ssh sup-uat-01 uptime   # run a remote command
```

In the VM TUI: `s` opens SSH, `c` runs checks, `enter` opens the web page,
`y` copies the SSH command, `/` edits the search, `?` shows help. Like the pod
TUI, editing the query filters results live; `r` reloads the inventory.

Inventory sources:

1. every concrete alias in `~/.ssh/config` (wildcards are not listed);
2. `servers` entries in the hop config.

SSH options (`HostName`, `User`, `Port`, `ProxyJump`) are read from
`~/.ssh/config`. Set `ssh_config` to use another file. `Include` files are
followed. Tags are explicit `servers[].tags` plus tokens derived from the name
and SSH alias (`sup-uat-vm-01` → `sup`, `uat`, `vm`, `01`).

### Configure VMs

```bash
hop config server add sup-uat-01 deploy@10.0.0.12 supervision uat vm
hop config server add db01 --ssh db01 --tags db,prod --health-url http://db01:8080/healthz
hop config server list
hop config server remove db01
```

The second positional argument is treated as `user@host` only when it contains
`@`; otherwise it is a tag. Use flags for explicit values:

```bash
hop config server add web01 \
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

If `metrics_url` is omitted, `hop` checks
`http://<host>:9100/metrics`. `health_url` is optional. Machine checks use a
read-only SSH script (`uptime`, load, CPU, `free`, `df`, `systemctl --failed`)
with `BatchMode=yes`. Disable them per server with `ssh_checks: false`.

The unified web UI also lists VMs when the query contains `vm`, but the
dedicated `/servers` page is the primary server view. Server detail pages are
served at `/server?name=…`; APIs are `/api/servers` and `/api/server` (add
`check=1` to run checks).

## Cockpit search

`hop all [terms...]` queries every enabled source in parallel and groups the
results. In a terminal it opens the cockpit TUI with live watch; otherwise it
prints grouped results. `--json` and `--plain` are available for scripting.

```bash
hop all api prod
hop all git hop
hop all pkg nginx
hop all ticket "checkout timeout"
hop all --json api | jq '.sections[].id'
hop all --plain "vm supervision"
```

| Source | ID | Selectors | Notes |
|---|---|---|---|
| Pods | `pods` | none | Kubernetes contexts |
| VMs / SSH | `servers` | `vm`, `vms` | `~/.ssh/config` and `servers` |
| Git repositories | `repos` | `git`, `repos` | Local discovery, GitHub/GitLab links |
| Packages | `packages` | `pkg`, `packages`, `registry` | GHCR, GitLab, Docker registry v2 |
| Tickets | `tickets` | `ticket`, `issue` | Network calls only when requested |
| Shortcuts | `shortcuts` | `link`, `links` | Configured browser links |

A selector focuses the search on one source. Without a selector, every enabled
source is queried. Tickets are local/no-op unless the query contains a ticket
selector or the web UI requests network search explicitly.

### Sources and plugins

Sources are enabled by default. Disable one without deleting its configuration:

```bash
hop config sources
hop config disable tickets
hop config disable pod link
hop config enable pods
hop config sources --json
```

This writes only `sources.disabled` in the YAML config, preserving comments:

```yaml
sources:
  disabled: [packages, tickets]
```

External command plugins are configured under `plugins` and implement the same
contract as built-ins. `hop` sends the JSON query on stdin and expects
`{"items": [...], "errors": [...], "meta": ...}` on stdout:

```yaml
plugins:
  - id: my-tools
    title: Team tools
    kind: tools
    selectors: [tool, tools]
    command: /home/me/bin/hop-tools
    args: [search]
    timeout: 5s
    env:
      TEAM: platform
```

```bash
echo '{"raw":"deploy","parsed":{"raw":"deploy","terms":["deploy"]},"limit":20}' \
  | /home/me/bin/hop-tools search
```

Plugin items are generic: `id`, `title`, `subtitle`, `description`, `badge`,
`url`, `copy`, `score`, `percent`, and `meta`. Toggle an external plugin with
`hop config disable my-tools`.

## Git repositories

`hop git [terms...]` discovers Git repositories under `git.roots` (defaults:
`~/dev`, `~/src`, `~/code`, `~/projects`) or explicit `git.repos`, with an
optional `git.exclude` list. It reads the current branch, last commit, author,
dirty files, ahead/behind state, and origin remote.

```bash
hop git hop
hop git --links hop
hop git --refresh --json
hop git branch:feature repo:web
```

GitHub and GitLab remotes get repository, branch, commit, pull/merge-request
and new-MR links. Override detection for self-hosted hosts:

```yaml
git:
  roots: [~/dev, ~/src]
  repos: []
  exclude: [vendor, archive]
  max_depth: 3
  cache_ttl: 30s
  providers:
    git.example.com: gitlab
```

## Package search

`hop pkg [terms...]` searches container image references. Images referenced by
pods are discovered automatically; add more under `packages.images`.
GHCR and GitLab container registries are configured by default.

```bash
hop pkg nginx
hop pkg ghcr
hop pkg --json api
```

```yaml
packages:
  images:
    - ghcr.io/SlimaineB/hop:main
    - registry.gitlab.com/group/project:latest
  cache_ttl: 5m
  timeout: 8s
  tag_limit: 50
  registries:
    - name: ghcr
      host: ghcr.io
      type: ghcr
    - name: gitlab
      host: registry.gitlab.com
      type: gitlab
      username_env: GITLAB_USER
      token_env: GITLAB_TOKEN
```

`username_env` / `token_env` enable bearer-token auth for private registries.
Tag fetching caches results; `search` never writes credentials to disk.

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
treat it as a flag: `hop` normalizes it to `last:2h` before parsing.

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
hop
hop web
hop web --bind 127.0.0.1 --port 8080
hop web --no-open 'api 12h 14h'
```

The UI listens on `127.0.0.1` by default. `--port 0` picks a free port.

The web UI has three clear entry points:

- `/` is the Kubernetes dashboard: pod readiness, restarts, CPU and memory from
  metrics-server, grouping by environment/application/cluster/namespace,
  per-group health, node capacity, and a triage list;
- `/servers` is the server dashboard: inventory, HTTP/SSH checks, memory, disk,
  failed units, and a local filter;
- `/search` is the unified search across pods, VMs, repositories, packages,
  tickets, and shortcuts.

Configured browser shortcuts still appear on the search page. A theme toggle in
the header switches between dark and light mode and remembers the choice in
`localStorage` (first visit follows the OS preference).

The dashboard APIs are:

- `/api/dashboard` for the Kubernetes overview (`?refresh=1` bypasses caches);
- `/api/dashboard/servers` for asynchronous server checks.

The UI also exposes:

- `/healthz` for health checks;
- `/api/unified` for grouped cockpit sections and live polling;
- `/api/search` for pod results;
- `/api/pod` for the full pod page;
- `/api/contexts` and `/api/stats`.

The pod page includes a topology graph (Ingress → Service → EndpointSlice →
Pod, owner chain, PVC, Node), containers, conditions, events, volumes, labels,
annotations, YAML, observability links, kubectl commands, and a detailed node
card. Click any topology node for facts and a copyable `kubectl` command.

## Commands

| Command | Purpose |
|---|---|
| `hop all [terms...]` | Unified cockpit TUI, aliases `cockpit`, `everything` |
| `hop git [terms...]` | Search local Git repos, aliases `repos`, `repo` |
| `hop pkg [terms...]` | Search container images, aliases `packages`, `registry` |
| `hop pod [terms...]` | Search pods, alias `pods`; TUI in a terminal |
| `hop vm [tags...]` | Search VMs, TUI in a terminal; aliases `servers`, `vms` |
| `hop link [keywords...]` | Open a configured browser shortcut, alias `links` |
| `hop issue [key\|terms...]` | Open, search or create tickets |
| `hop issue new [title...]` | Create a ticket, optional `--from-pod` / `--from-vm` |
| `hop issue providers` | List configured issue providers |
| `hop <provider> ...` | Provider alias, e.g. `hop jira OPS-123` |
| `hop [terms...]` | Legacy root search: pods, or VMs with the `vm` selector |
| `hop` | Local web UI |
| `hop web [terms...]` | Web UI, aliases `ui`, `serve` |
| `hop search [terms...]` | Non-interactive pod search, alias `find` |
| `hop server NAME` | Show one VM, add `--check` or `--json` |
| `hop ssh NAME [command...]` | Open SSH or run a remote command |
| `hop config server add NAME ...` | Add or update a VM |
| `hop config server list` | List discovered VMs/servers |
| `hop config server remove NAME` | Remove a config VM entry |
| `hop config shortcut add NAME URL ...` | Add or update a browser shortcut |
| `hop config shortcut list` | List browser shortcuts |
| `hop config shortcut remove NAME` | Remove a browser shortcut |
| `hop contexts` | List contexts, alias `ctx` |
| `hop config init` | Create an example configuration |
| `hop config path` | Print the effective config path |
| `hop config show` | Print the resolved config |
| `hop config check` | Validate config and kubeconfigs |
| `hop config sources` | List sources and enabled state, aliases `plugins`, `options` |
| `hop config enable SOURCE...` | Enable one or more sources/plugins |
| `hop config disable SOURCE...` | Disable one or more sources/plugins |
| `hop cache dir` | Print the cache directory |
| `hop cache clear` | Delete cached snapshots |
| `hop version` | Print the version |

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
| `--plain` | One line per pod for `search`, or per result for `all` |
| `--live` | Live watch in `hop all` (default true) |
| `--tickets` | Force network ticket searches in `hop all` |
| `--links` | Print repo/branch/commit/MR links for `git` |

## Configuration

Resolution order:

1. `--config <path>`;
2. `$HOP_CONFIG`;
3. `$XDG_CONFIG_HOME/hop/config.yaml`;
4. `~/.config/hop/config.yaml`.

Without a config file, the defaults are enough.

```bash
hop config init
hop config show
hop config check
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

sources:
  disabled: []   # e.g. [tickets, packages]

git:
  roots: [~/dev, ~/src]
  max_depth: 3
  cache_ttl: 30s

packages:
  cache_ttl: 5m
  tag_limit: 50
  registries:
    - {name: ghcr, host: ghcr.io, type: ghcr}
    - {name: gitlab, host: registry.gitlab.com, type: gitlab}

plugins: []     # external command sources
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
by field. If `env` is not set, `hop` infers it from the context name (`prod`,
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

`hop issue` never asks for `--provider`. It detects ticket keys, uses the
configured default provider, or prompts a menu when several providers match.

```bash
hop issue OPS-123              # open a ticket
hop issue "checkout timeout"   # search all configured providers
hop issue new "Pod api crashloop" --from-pod api-123
hop issue new "VM disk full" --from-vm sup-uat-01
hop issue providers
```

Provider aliases work directly and are handy when several providers exist:

```bash
hop jira OPS-123
hop jira search "checkout timeout"
hop jira new "Pod api crashloop"
hop jira                        # recent tickets
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

Tokens are read from the environment, never from the hop config. Jira uses the
REST API v2 (`/rest/api/2`). Opening a ticket only needs the base URL; search and
create need the API token. Adding another provider means implementing
`internal/providers.TicketProvider`, registering a factory, and adding its
config block. The CLI, TUI, and web UI stay unchanged.

## Browser shortcuts

`hop link` opens a configured URL by keyword. It is separate from the pod
observability links: shortcuts are global browser bookmarks.

```bash
hop link grafana
hop link ticket
hop link --list
hop link --print jira
hop config shortcut add swagger https://api.example.com/swagger api openapi
hop config shortcut add jira https://jira.example.com --keywords jira,ticket,bug
hop config shortcut list
hop config shortcut remove jira
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
- An on-disk snapshot is kept in `os.UserCacheDir()/hop`
  (usually `~/.cache/hop`).
- `cache_ttl: 0` disables the cache.
- `hop cache dir` and `hop cache clear` manage the directory.
- The on-disk cache is not written if a cluster returned an error.

## Architecture

```text
cmd/hop              entry point
internal/cli          Cobra, flags, commands
internal/config       YAML config, workspaces, resolution, templates
internal/kube         kubeconfig, contexts, pods, nodes, client-go
internal/search       tag and time parsing, fuzzy engine
internal/app          snapshots, cache, ranking, orchestration
internal/dashboard    Kubernetes overview, metrics, grouping, server checks
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
go build -ldflags "-X github.com/SlimaineB/hop/internal/version.Version=v0.1.0" \
  -o hop ./cmd/hop
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

`hop` does not copy your Kubernetes credentials or SSH keys: it uses your
kubeconfigs and SSH agent/config directly. The config created by `config init`
is `0600`. The cache contains pod metadata, not Kubernetes secrets. Machine
checks only run read-only inspection commands. Browser shortcuts only accept
`http` and `https` URLs and open them with the default browser. Issue tokens are
read from environment variables and are never written to the config or logs.
