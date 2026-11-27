#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
LAB="$(pwd)"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for the lab" >&2
  exit 1
fi
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "docker compose is required for the lab" >&2
  exit 1
fi

mkdir -p keys
if [ ! -f keys/id_ed25519 ]; then
  ssh-keygen -q -t ed25519 -N "" -C "hop-lab" -f "$LAB/keys/id_ed25519"
fi
chmod 0600 keys/id_ed25519
chmod 0644 keys/id_ed25519.pub

cat > "$LAB/ssh_config" <<EOF
Host lab-ssh
  HostName 127.0.0.1
  Port 2222
  User hop
  IdentityFile $LAB/keys/id_ed25519
  IdentitiesOnly yes
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
EOF
chmod 0600 "$LAB/ssh_config"

cat > "$LAB/hop.yaml" <<EOF
ssh_config: $LAB/ssh_config

servers:
  - name: lab-ssh
    ssh: lab-ssh
    env: lab
    tags: [lab, ssh]
    check_ports: [2222]

issues:
  default_provider: jira-lab

providers:
  jira-lab:
    type: jira
    base_url: http://127.0.0.1:8081
    token_env: JIRA_TOKEN
    auth_scheme: bearer
    keywords: [jira, lab]
    defaults:
      project: LAB
  gitlab-lab:
    type: gitlab
    base_url: http://127.0.0.1:8082
    token_env: GITLAB_TOKEN
    auth_scheme: private-token
    keywords: [gitlab, lab]
    defaults:
      project: lab/hop
EOF

cat > "$LAB/env.sh" <<EOF
export HOP_CONFIG="$LAB/hop.yaml"
export JIRA_TOKEN=lab-token
export GITLAB_TOKEN=lab-token
EOF
chmod 0600 "$LAB/env.sh"

"${COMPOSE[@]}" -f "$LAB/docker-compose.yml" up -d --build

for _ in $(seq 1 30); do
  if timeout 1 bash -c "</dev/tcp/127.0.0.1/8081" 2>/dev/null \
    && timeout 1 bash -c "</dev/tcp/127.0.0.1/8082" 2>/dev/null \
    && timeout 1 bash -c "</dev/tcp/127.0.0.1/2222" 2>/dev/null; then
    break
  fi
  sleep 1
done

echo
echo "hop lab is ready."
echo
echo "  source $LAB/env.sh"
echo
echo "  hop issue LAB-1 --print"
echo "  hop issue new \"Lab issue\" --print"
echo "  hop gitlab \"checkout\""
echo "  hop gitlab 1 --print"
echo "  hop gitlab new \"GitLab lab issue\" --print"
echo "  hop vm lab"
echo "  hop server lab-ssh --check"
echo "  hop ssh lab-ssh uptime"
echo "  hop ctr --running"
echo
echo "Config: $LAB/hop.yaml"
echo "SSH:    ssh -F $LAB/ssh_config lab-ssh"
echo "Stop:   $LAB/down.sh"
