#!/usr/bin/env bash
set -euo pipefail

APP_NAME="factoryx-helpdesk"
SERVICE_NAME="factoryx-helpdesk"
INSTALL_ROOT="/opt/factoryx-helpdesk"
ENV_DEST="/etc/factoryx-helpdesk/help-desk.env"
APP_USER="factoryx-helpdesk"
REMOTE_HOST=""
SSH_PORT="22"

usage() {
  cat <<'USAGE'
FactoryX Help Desk installer

Usage:
  ./scripts/install.sh [--remote user@host] [--ssh-port 22]

Examples:
  ./scripts/install.sh
  ./scripts/install.sh --remote ubuntu@203.0.113.20 --ssh-port 22
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote)
      REMOTE_HOST="${2:-}"
      shift 2
      ;;
    --ssh-port)
      SSH_PORT="${2:-22}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build the server binary." >&2
  exit 1
fi

if [[ ! -f "./go.mod" ]]; then
  echo "Run this installer from the project root." >&2
  exit 1
fi

prompt_required() {
  local name="$1"
  local prompt="$2"
  local secret="${3:-false}"
  local value=""
  while [[ -z "$value" ]]; do
    if [[ "$secret" == "true" ]]; then
      read -r -s -p "$prompt: " value
      echo
    else
      read -r -p "$prompt: " value
    fi
    value="${value//[$'\t\r\n']/}"
  done
  printf -v "$name" "%s" "$value"
}

prompt_optional() {
  local name="$1"
  local prompt="$2"
  local default_value="$3"
  local value=""
  read -r -p "$prompt [$default_value]: " value
  value="${value:-$default_value}"
  printf -v "$name" "%s" "$value"
}

echo "== FactoryX Help Desk Installer =="
echo

echo "Select database backend:"
echo "  1) PostgreSQL"
echo "  2) MariaDB"
DB_CHOICE=""
while [[ "$DB_CHOICE" != "1" && "$DB_CHOICE" != "2" ]]; do
  read -r -p "Choice (1/2): " DB_CHOICE
done

if [[ "$DB_CHOICE" == "1" ]]; then
  STORE_BACKEND="postgres"
  prompt_optional DB_HOST "PostgreSQL host" "127.0.0.1"
  prompt_optional DB_PORT "PostgreSQL port" "5432"
  prompt_required DB_NAME "PostgreSQL database name"
  prompt_required DB_USER "PostgreSQL username"
  prompt_required DB_PASS "PostgreSQL password" true
  DB_DSN="postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable"
else
  STORE_BACKEND="mariadb"
  prompt_optional DB_HOST "MariaDB host" "127.0.0.1"
  prompt_optional DB_PORT "MariaDB port" "3306"
  prompt_required DB_NAME "MariaDB database name"
  prompt_required DB_USER "MariaDB username"
  prompt_required DB_PASS "MariaDB password" true
  DB_DSN="${DB_USER}:${DB_PASS}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}?parseTime=true&multiStatements=true"
fi

prompt_optional APP_PORT "Application HTTP port" "8080"
prompt_required ADMIN_EMAIL "Initial admin email"
prompt_required ADMIN_PASSWORD "Initial admin password (min 8 chars)" true

echo
echo "SMTP configuration for password reset emails"
prompt_required SMTP_HOST "SMTP host"
prompt_optional SMTP_PORT "SMTP port" "587"
prompt_optional SMTP_USER "SMTP username (blank uses anonymous send)" ""
if [[ -n "$SMTP_USER" ]]; then
  prompt_required SMTP_PASS "SMTP password" true
else
  SMTP_PASS=""
fi
prompt_required SMTP_FROM "SMTP from address"
prompt_required SMTP_RESET_URL_BASE "Password reset URL base (example: https://helpdesk.example.com/reset-password)"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ENV_FILE="$TMP_DIR/help-desk.env"
cat > "$ENV_FILE" <<EOF
PORT=${APP_PORT}
STORE_BACKEND=${STORE_BACKEND}
DB_DSN=${DB_DSN}
ADMIN_EMAIL=${ADMIN_EMAIL}
ADMIN_PASSWORD=${ADMIN_PASSWORD}
SMTP_HOST=${SMTP_HOST}
SMTP_PORT=${SMTP_PORT}
SMTP_USER=${SMTP_USER}
SMTP_PASS=${SMTP_PASS}
SMTP_FROM=${SMTP_FROM}
SMTP_RESET_URL_BASE=${SMTP_RESET_URL_BASE}
EOF

SERVICE_FILE="$TMP_DIR/${SERVICE_NAME}.service"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=FactoryX Help Desk Service
After=network.target

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${INSTALL_ROOT}
EnvironmentFile=${ENV_DEST}
ExecStart=${INSTALL_ROOT}/bin/help-desk
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

BIN_FILE="$TMP_DIR/help-desk"
echo
echo "Building binary..."
go build -o "$BIN_FILE" ./cmd/server

run_remote_install() {
  local host="$1"
  local port="$2"

  if ! command -v scp >/dev/null 2>&1 || ! command -v ssh >/dev/null 2>&1; then
    echo "ssh and scp are required for remote installation." >&2
    exit 1
  fi

  scp -P "$port" "$BIN_FILE" "$ENV_FILE" "$SERVICE_FILE" "$host:/tmp/"

  ssh -p "$port" "$host" "bash -s" <<'REMOTE_SCRIPT'
set -euo pipefail
APP_USER="factoryx-helpdesk"
INSTALL_ROOT="/opt/factoryx-helpdesk"
ENV_DEST="/etc/factoryx-helpdesk/help-desk.env"
SERVICE_NAME="factoryx-helpdesk.service"

sudo useradd --system --home /var/lib/factoryx-helpdesk --shell /usr/sbin/nologin "$APP_USER" 2>/dev/null || true
sudo mkdir -p "$INSTALL_ROOT/bin" /etc/factoryx-helpdesk
sudo install -m 0755 /tmp/help-desk "$INSTALL_ROOT/bin/help-desk"
sudo install -m 0640 /tmp/help-desk.env "$ENV_DEST"
sudo chown root:"$APP_USER" "$ENV_DEST"
sudo install -m 0644 /tmp/factoryx-helpdesk.service "/etc/systemd/system/$SERVICE_NAME"
sudo systemctl daemon-reload
sudo systemctl enable --now factoryx-helpdesk
sudo rm -f /tmp/help-desk /tmp/help-desk.env /tmp/factoryx-helpdesk.service
REMOTE_SCRIPT
}

run_local_install() {
  sudo useradd --system --home /var/lib/factoryx-helpdesk --shell /usr/sbin/nologin "$APP_USER" 2>/dev/null || true
  sudo mkdir -p "$INSTALL_ROOT/bin" /etc/factoryx-helpdesk
  sudo install -m 0755 "$BIN_FILE" "$INSTALL_ROOT/bin/help-desk"
  sudo install -m 0640 "$ENV_FILE" "$ENV_DEST"
  sudo chown root:"$APP_USER" "$ENV_DEST"
  sudo install -m 0644 "$SERVICE_FILE" "/etc/systemd/system/${SERVICE_NAME}.service"
  sudo systemctl daemon-reload
  sudo systemctl enable --now "$SERVICE_NAME"
}

if [[ -n "$REMOTE_HOST" ]]; then
  echo
  echo "Installing to remote host: $REMOTE_HOST"
  run_remote_install "$REMOTE_HOST" "$SSH_PORT"
else
  echo
  echo "Installing on local Ubuntu machine"
  run_local_install
fi

echo
echo "Installation complete."
if [[ -n "$REMOTE_HOST" ]]; then
  echo "Check status with: ssh -p $SSH_PORT $REMOTE_HOST 'sudo systemctl status ${SERVICE_NAME}'"
else
  echo "Check status with: sudo systemctl status ${SERVICE_NAME}"
fi
