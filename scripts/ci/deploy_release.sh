#!/usr/bin/env bash
set -euo pipefail

PACKAGE_PATH="${1:-hysteria2-plan.tar.gz}"

test -f "$PACKAGE_PATH" || {
  echo "部署包不存在: $PACKAGE_PATH"
  exit 1
}

DEPLOY_PORT="${DEPLOY_PORT:-22}"
DEPLOY_KEEP_RELEASES="${DEPLOY_KEEP_RELEASES:-5}"
PANEL_SERVICE_NAME="${PANEL_SERVICE_NAME:-mxinhy-panel}"
PACKAGE_NAME="package-${CI_COMMIT_SHA}.tar.gz"
REMOTE_PACKAGE="/tmp/${PACKAGE_NAME}"
REMOTE_RELEASE_DIR="${DEPLOY_PATH}/releases/${CI_COMMIT_SHA}"
REMOTE_CURRENT_LINK="${DEPLOY_PATH}/current"
REMOTE_SHARED_DIR="${DEPLOY_PATH}/shared"

SSH_TARGET="${DEPLOY_USER}@${DEPLOY_HOST}"
SSH_OPTS=(-p "$DEPLOY_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes)
SCP_OPTS=(-P "$DEPLOY_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes)

scp "${SCP_OPTS[@]}" "$PACKAGE_PATH" "${SSH_TARGET}:${REMOTE_PACKAGE}"

ssh "${SSH_OPTS[@]}" "$SSH_TARGET" \
  CI_COMMIT_SHA="$CI_COMMIT_SHA" \
  REMOTE_PACKAGE="$REMOTE_PACKAGE" \
  REMOTE_RELEASE_DIR="$REMOTE_RELEASE_DIR" \
  REMOTE_CURRENT_LINK="$REMOTE_CURRENT_LINK" \
  REMOTE_SHARED_DIR="$REMOTE_SHARED_DIR" \
  DEPLOY_PATH="$DEPLOY_PATH" \
  DEPLOY_KEEP_RELEASES="$DEPLOY_KEEP_RELEASES" \
  PANEL_SERVICE_NAME="$PANEL_SERVICE_NAME" \
  'bash -s' <<'EOF'
set -euo pipefail

mkdir -p "${DEPLOY_PATH}/releases" "${REMOTE_SHARED_DIR}/config" "${REMOTE_SHARED_DIR}/storage"
rm -rf "${REMOTE_RELEASE_DIR}"
mkdir -p "${REMOTE_RELEASE_DIR}"

tar -xzf "${REMOTE_PACKAGE}" -C "${REMOTE_RELEASE_DIR}"
rm -f "${REMOTE_PACKAGE}"

rm -rf "${REMOTE_RELEASE_DIR}/storage"
ln -sfn "${REMOTE_SHARED_DIR}/storage" "${REMOTE_RELEASE_DIR}/storage"

PANEL_ENV_PATH="${REMOTE_SHARED_DIR}/config/panel.env"
PANEL_BINARY_PATH="${REMOTE_RELEASE_DIR}/build/panel/mxinhy-panel"
PANEL_SERVICE_TEMPLATE="${REMOTE_RELEASE_DIR}/deploy/systemd/mxinhy-panel.service"

if [ ! -f "${PANEL_ENV_PATH}" ] && [ -f "${REMOTE_RELEASE_DIR}/config/panel.env.example" ]; then
  cp "${REMOTE_RELEASE_DIR}/config/panel.env.example" "${PANEL_ENV_PATH}"
fi

ln -sfn "${REMOTE_RELEASE_DIR}" "${REMOTE_CURRENT_LINK}"

if command -v systemctl >/dev/null 2>&1; then
  if [ -x "${PANEL_BINARY_PATH}" ] && [ -f "${PANEL_SERVICE_TEMPLATE}" ]; then
    PANEL_UNIT_PATH="/etc/systemd/system/${PANEL_SERVICE_NAME}.service"
    sed \
      -e "s|{{PANEL_INSTALL_PATH}}|${PANEL_BINARY_PATH}|g" \
      -e "s|{{PANEL_ENV_PATH}}|${PANEL_ENV_PATH}|g" \
      -e "s|{{PANEL_WORKDIR}}|${REMOTE_RELEASE_DIR}|g" \
      "${PANEL_SERVICE_TEMPLATE}" > "${PANEL_UNIT_PATH}"
    chmod 644 "${PANEL_UNIT_PATH}"
    systemctl daemon-reload
    systemctl enable "${PANEL_SERVICE_NAME}"
    systemctl restart "${PANEL_SERVICE_NAME}"
  fi
fi

CURRENT_RELEASE_DIR="$(readlink -f "${REMOTE_CURRENT_LINK}" 2>/dev/null || true)"
mapfile -t RELEASE_DIRS < <(
  find "${DEPLOY_PATH}/releases" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' \
    | sort -nr \
    | awk '{print $2}'
)

if [ "${#RELEASE_DIRS[@]}" -gt "${DEPLOY_KEEP_RELEASES}" ]; then
  for RELEASE_DIR in "${RELEASE_DIRS[@]:${DEPLOY_KEEP_RELEASES}}"; do
    if [ -n "${CURRENT_RELEASE_DIR}" ] && [ "${RELEASE_DIR}" = "${CURRENT_RELEASE_DIR}" ]; then
      continue
    fi
    rm -rf "${RELEASE_DIR}"
  done
fi
EOF
