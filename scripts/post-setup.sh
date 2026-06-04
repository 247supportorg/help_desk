#!/usr/bin/env bash
# post-setup.sh
#
# Setup sonrası server'ı SQL modunda yeniden başlatır ve gizli bilgileri siler.
#
# Kullanım:
#   ./scripts/post-setup.sh                      # ./help-desk.env kullanır
#   ./scripts/post-setup.sh /path/to/env-file    # belirtilen env dosyası
#   ./scripts/post-setup.sh --binary /tmp/app    # binary yolunu override
#   ./scripts/post-setup.sh --keep-env           # help-desk.env'i silme
#   ./scripts/post-setup.sh --keep-snapshot      # help-desk.setup.enc'i silme

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

ENV_FILE=""
BINARY=""
KEEP_ENV=0
KEEP_SNAPSHOT=0
LOG_FILE="/tmp/helpdesk.log"

usage() {
  cat <<'USAGE'
FactoryX Help Desk post-setup

Setup sonrası:
  1. help-desk.env'den DB_DSN ve SMTP ayarlarını okur
  2. Çalışan server'ı durdurur
  3. Yeni server'ı SQL modunda başlatır
  4. Login'i test eder
  5. Gizli bilgileri siler (help-desk.env, snapshot, /tmp log/cert)

Usage:
  ./scripts/post-setup.sh [env-file] [flags]

Flags:
  --binary <path>       Server binary yolu (default: proje içindeki ./server veya /tmp/help-desk-new)
  --keep-env            help-desk.env dosyasını silme
  --keep-snapshot       help-desk.setup.enc dosyasını silme
  --log <path>          Log dosyası (default: /tmp/helpdesk.log)
  -h, --help            Bu yardım
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      BINARY="${2:-}"
      shift 2
      ;;
    --keep-env)
      KEEP_ENV=1
      shift
      ;;
    --keep-snapshot)
      KEEP_SNAPSHOT=1
      shift
      ;;
    --log)
      LOG_FILE="${2:-/tmp/helpdesk.log}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "Unknown flag: $1" >&2
      usage
      exit 2
      ;;
    *)
      ENV_FILE="$1"
      shift
      ;;
  esac
done

# Env dosyası default
if [[ -z "$ENV_FILE" ]]; then
  ENV_FILE="$PROJECT_ROOT/help-desk.env"
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: env file not found: $ENV_FILE" >&2
  echo "  ./scripts/post-setup.sh /path/to/help-desk.env" >&2
  exit 1
fi

# Binary default
if [[ -z "$BINARY" ]]; then
  for candidate in \
    "$PROJECT_ROOT/server" \
    "$PROJECT_ROOT/bin/help-desk" \
    /tmp/help-desk-new \
    /usr/local/bin/help-desk \
    /opt/factoryx-helpdesk/bin/help-desk; do
    if [[ -x "$candidate" ]]; then
      BINARY="$candidate"
      break
    fi
  done
  if [[ -z "$BINARY" ]]; then
    echo "ERROR: server binary not found. Build with: go build -o $PROJECT_ROOT/server ./cmd/server" >&2
    exit 1
  fi
fi

# Env dosyasını oku, değerleri export et
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

if [[ -z "${DB_DSN:-}" ]]; then
  echo "ERROR: DB_DSN missing in $ENV_FILE" >&2
  exit 1
fi

if [[ -z "${ADMIN_EMAIL:-}" || -z "${ADMIN_PASSWORD:-}" ]]; then
  echo "ERROR: ADMIN_EMAIL or ADMIN_PASSWORD missing in $ENV_FILE" >&2
  exit 1
fi

echo "==> Env: $ENV_FILE"
echo "==> Binary: $BINARY"
echo "==> DB: ${DB_DSN%%\?*}"
echo "==> Admin: $ADMIN_EMAIL"
echo

# 1. Eski server'ı durdur
echo "==> Stopping existing server..."
BINARY_NAME=$(basename "$BINARY")
# Önce port'u dinleyen process'i bul, sonra binary path'i ile eşle
PIDS=""
if command -v fuser >/dev/null 2>&1; then
  PORT_PID=$(fuser 8080/tcp 2>/dev/null | tr -d ' ' || true)
  if [[ -n "$PORT_PID" ]]; then
    PIDS="$PIDS $PORT_PID"
  fi
fi
# Binary path'inin tam eşleşmesiyle çalışan process'leri bul
for pid_dir in /proc/[0-9]*; do
  pid=$(basename "$pid_dir")
  if [[ -L "$pid_dir/exe" ]]; then
    exe_target=$(readlink "$pid_dir/exe" 2>/dev/null || true)
    if [[ "$exe_target" == "$BINARY" ]]; then
      PIDS="$PIDS $pid"
    fi
  fi
  # Komut satırında binary adı geçen process'leri yakala (kendi scriptimiz hariç)
  if [[ -r "$pid_dir/cmdline" ]]; then
    cmdline=$(tr '\0' ' ' < "$pid_dir/cmdline" 2>/dev/null || true)
    case "$cmdline" in
      *"$BINARY"*|*"help-desk-new"*|*"$BINARY_NAME"*)
        # kendi scriptimizin PID'si ve shell PID'si hariç tut
        if [[ "$pid" != "$$" && "$pid" != "$PPID" ]]; then
          PIDS="$PIDS $pid"
        fi
        ;;
    esac
  fi
done
PIDS=$(echo "$PIDS" | tr ' ' '\n' | sort -un | tr '\n' ' ' | xargs)

if [[ -n "$PIDS" ]]; then
  echo "    killing PIDs: $PIDS"
  for p in $PIDS; do
    kill "$p" 2>/dev/null || true
  done
  sleep 2
  for p in $PIDS; do
    if kill -0 "$p" 2>/dev/null; then
      kill -9 "$p" 2>/dev/null || true
    fi
  done
  sleep 1
  echo "    stopped"
else
  echo "    no running instance"
fi

# 2. Yeni server'ı SQL modunda başlat
echo "==> Starting server in SQL mode..."
SESSION_SECRET_VAL="${SESSION_SECRET:-$(head -c 32 /dev/urandom | base64)}"
SESSION_SECRET="$SESSION_SECRET_VAL" \
STORE_BACKEND="${STORE_BACKEND:-postgres}" \
DB_DSN="$DB_DSN" \
SMTP_HOST="${SMTP_HOST:-}" \
SMTP_PORT="${SMTP_PORT:-587}" \
SMTP_USER="${SMTP_USER:-}" \
SMTP_PASS="${SMTP_PASS:-}" \
SMTP_FROM="${SMTP_FROM:-}" \
SMTP_RESET_URL_BASE="${SMTP_RESET_URL_BASE:-}" \
nohup "$BINARY" > "$LOG_FILE" 2>&1 &
disown
sleep 2

NEW_PID=$(pgrep -f "$BINARY" | head -1)
if [[ -z "$NEW_PID" ]]; then
  echo "ERROR: server failed to start. See $LOG_FILE" >&2
  exit 1
fi
echo "    started PID $NEW_PID"

# 3. Log kontrol
if ! grep -q "listening on" "$LOG_FILE" 2>/dev/null; then
  echo "WARNING: server log doesn't show 'listening on' yet. Check $LOG_FILE"
  tail -5 "$LOG_FILE" 2>/dev/null || true
fi

# 4. Login testi
echo "==> Testing login..."
LOGIN_BODY=$(mktemp)
HTTP_CODE=$(curl -s -o "$LOGIN_BODY" -w "%{http_code}" -X POST http://127.0.0.1:8080/api/auth/login \
  -H "Content-Type: application/json" \
  --data-binary "$(printf '{"email":"%s","password":"%s"}' "$ADMIN_EMAIL" "$ADMIN_PASSWORD")")
LOGIN_RESULT=$(cat "$LOGIN_BODY")
rm -f "$LOGIN_BODY"

if [[ "$HTTP_CODE" == "200" ]]; then
  echo "    OK (HTTP 200): $LOGIN_RESULT"
else
  echo "    FAILED (HTTP $HTTP_CODE): $LOGIN_RESULT" >&2
  echo "    Check log: $LOG_FILE" >&2
fi

# 5. Gizli bilgileri sil
echo "==> Cleaning up secrets..."

if [[ "$KEEP_ENV" -eq 0 ]]; then
  rm -f "$ENV_FILE"
  echo "    deleted: $ENV_FILE"
else
  echo "    kept:    $ENV_FILE (--keep-env)"
fi

if [[ "$KEEP_SNAPSHOT" -eq 0 ]]; then
  rm -f "$PROJECT_ROOT/help-desk.setup.enc"
  echo "    deleted: $PROJECT_ROOT/help-desk.setup.enc"
else
  echo "    kept:    $PROJECT_ROOT/help-desk.setup.enc (--keep-snapshot)"
fi

rm -f /tmp/c.pem /tmp/k.pem /tmp/*.txt /tmp/snap_req.json /tmp/test-setup.enc 2>/dev/null || true
echo "    deleted: /tmp test artifacts"

# 6. Server ayakta, gizli bilgi yok
echo
echo "==> Done."
echo "    Server PID: $NEW_PID"
echo "    Admin:      $ADMIN_EMAIL"
echo "    URL:        http://127.0.0.1:${PORT:-8080}/admin/login"
echo "    Log:        $LOG_FILE"
