#!/usr/bin/env bash
set -euo pipefail

mkdir -p ~/.ssh
chmod 700 ~/.ssh

printf '%s\n' "$SSH_PRIVATE_KEY" | tr -d '\r' > ~/.ssh/id_ed25519
chmod 600 ~/.ssh/id_ed25519

if [ -n "${DEPLOY_KNOWN_HOSTS:-}" ]; then
  printf '%s\n' "$DEPLOY_KNOWN_HOSTS" > ~/.ssh/known_hosts
else
  ssh-keyscan -p "${DEPLOY_PORT:-22}" -H "$DEPLOY_HOST" > ~/.ssh/known_hosts
fi

chmod 644 ~/.ssh/known_hosts
