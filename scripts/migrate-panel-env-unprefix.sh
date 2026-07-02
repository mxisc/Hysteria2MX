#!/usr/bin/env bash
set -euo pipefail

ENV_PATH="${1:-config/panel.env}"

if [ ! -f "$ENV_PATH" ]; then
  echo "panel env file not found: $ENV_PATH" >&2
  exit 1
fi

map_key() {
  case "$1" in
    PANEL_BIND_ADDR) echo "BIND_ADDR" ;;
    PANEL_STATIC_DIR) echo "STATIC_DIR" ;;
    PANEL_ENV) echo "ENV" ;;
    PANEL_SESSION_NAME) echo "SESSION_NAME" ;;
    PANEL_PUBLIC_API_BASE_URL) echo "PUBLIC_API_BASE_URL" ;;
    PANEL_ENCRYPTION_KEY) echo "ENCRYPTION_KEY" ;;
    PANEL_LOGIN_AES_SEED) echo "LOGIN_AES_SEED" ;;
    PANEL_LOG_LEVEL) echo "LOG_LEVEL" ;;
    PANEL_*) echo "${1#PANEL_}" ;;
    *) echo "$1" ;;
  esac
}

backup_path="${ENV_PATH}.bak-$(date +%Y%m%d%H%M%S)"
cp -p "$ENV_PATH" "$backup_path"

tmp_path="$(mktemp "${ENV_PATH}.tmp.XXXXXX")"
declare -A seen_new_keys=()

while IFS= read -r line || [ -n "$line" ]; do
  if [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*= ]]; then
    key="${BASH_REMATCH[2]}"
    mapped_key="$(map_key "$key")"
    if [ "$key" = "$mapped_key" ]; then
      seen_new_keys["$key"]=1
    fi
  fi
done < "$ENV_PATH"

while IFS= read -r line || [ -n "$line" ]; do
  if [[ "$line" =~ ^([[:space:]]*)(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)([[:space:]]*=.*)$ ]]; then
    leading="${BASH_REMATCH[1]}"
    export_prefix="${BASH_REMATCH[2]}"
    key="${BASH_REMATCH[3]}"
    suffix="${BASH_REMATCH[4]}"
    mapped_key="$(map_key "$key")"
    if [ "$key" != "$mapped_key" ]; then
      if [ "${seen_new_keys[$mapped_key]+set}" = "set" ]; then
        continue
      fi
      seen_new_keys["$mapped_key"]=1
      printf '%s%s%s%s\n' "$leading" "$export_prefix" "$mapped_key" "$suffix" >> "$tmp_path"
      continue
    fi
  fi
  printf '%s\n' "$line" >> "$tmp_path"
done < "$ENV_PATH"

mv "$tmp_path" "$ENV_PATH"
echo "migrated $ENV_PATH"
echo "backup: $backup_path"
