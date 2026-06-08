# FactoryX Help Desk

(work in progress and will be written from scrach without ai-code)

A web-based Help Desk (Technical Support Software) project with a Go backend and ticket workflow for resolving customer requests and technical incidents.

## Features

- Create support tickets with customer details, priority, and issue description
- Track ticket lifecycle: `open`, `in_progress`, `resolved`, `closed`
- Assign agents and add resolution notes
- Add comments (including internal notes)
- Filter tickets by status
- View dashboard counts for ticket statuses
- Single binary Go backend serving both API and web UI

## Tech Stack

- Backend: Go (`net/http`, repository pattern)
- Storage backends:
  - In-memory store (`sync.RWMutex`)
  - PostgreSQL (`database/sql` + `github.com/lib/pq`)
  - MariaDB (`database/sql` + `github.com/go-sql-driver/mysql`)
- Frontend: HTML, CSS, Vanilla JavaScript

## Run Locally

### 1) Run with in-memory store (default)

```bash
go run ./cmd/server
```

Server starts at `http://localhost:8080`.

### 2) Start local databases with Docker Compose

The bundled `docker-compose.yml` reads database credentials from your environment (or a local `.env` file) and refuses to start if `POSTGRES_PASSWORD`, `MARIADB_PASSWORD`, and `MARIADB_ROOT_PASSWORD` are not set. Create a local `.env` file (kept out of git by `.gitignore`):

```bash
cat > .env <<'EOF'
POSTGRES_USER=helpdesk
POSTGRES_PASSWORD=change-me-postgres
MARIADB_USER=helpdesk
MARIADB_PASSWORD=change-me-mariadb
MARIADB_ROOT_PASSWORD=change-me-mariadb-root
EOF
```

Then start the services:

```bash
docker compose up -d
```

This starts:

- PostgreSQL on `localhost:5432` (db/user: `helpdesk` by default)
- MariaDB on `localhost:3306` (db/user: `helpdesk` by default)

If `docker compose` is not available on your Ubuntu install, use `docker-compose` with the same arguments.
If you get `Permission denied` for the Docker socket, run the command with `sudo` or add your user to the `docker` group and log out/in:

```bash
sudo usermod -aG docker $USER
```

If port `3306` or `5432` is already in use on your machine, choose another host port:

```bash
sudo env MARIADB_HOST_PORT=3307 docker-compose up -d mariadb
sudo env POSTGRES_HOST_PORT=5433 docker-compose up -d postgres
```

If `docker-compose` v1 throws a `ContainerConfig` error while recreating containers, clean up the old project state and start again:

```bash
sudo docker-compose down -v
sudo docker rm -f helpdesk-mariadb
sudo docker-compose up -d mariadb
```

If PostgreSQL or MariaDB returns `password authentication failed`, the database volume may still contain the old password state. Remove the volume and recreate the container with the matching password:

```bash
sudo docker-compose down -v
sudo docker-compose up -d postgres
```

### 3) Run with PostgreSQL

```bash
STORE_BACKEND=postgres \
DB_DSN="postgres://<DB_USER>:<DB_PASSWORD>@localhost:5432/<DB_NAME>?sslmode=disable" \
go run ./cmd/server
```

### 4) Run with MariaDB

```bash
STORE_BACKEND=mariadb \
DB_DSN="<DB_USER>:<DB_PASSWORD>@tcp(127.0.0.1:3306)/<DB_NAME>?parseTime=true&multiStatements=true" \
go run ./cmd/server
```

The app automatically creates required tables on startup.

## Ubuntu Installer (Local or Remote)

An interactive installer is included for Ubuntu machines and Ubuntu servers.

Run on local Ubuntu machine:

```bash
make install-ubuntu
```

Run from your local machine to install on remote Ubuntu server:

```bash
make install-ubuntu-remote REMOTE=ubuntu@your-server-ip SSH_PORT=22
```

Installer capabilities:

- Installs binary to `/opt/factoryx-helpdesk/bin/help-desk`
- Creates systemd service `factoryx-helpdesk`
- Creates env file at `/etc/factoryx-helpdesk/help-desk.env`
- Lets you choose PostgreSQL or MariaDB during install
- Prompts for admin email/password
- Prompts for SMTP settings used by password reset emails

After installation:

```bash
sudo systemctl status factoryx-helpdesk
sudo journalctl -u factoryx-helpdesk -f
```

## Web Setup Wizard

You can also use a browser-based setup page (Forgejo-style first-run form):

```bash
go run ./cmd/server
```

Open:

- `http://localhost:8080/setup`

The setup wizard includes fields for:

- Database backend selection (`PostgreSQL` or `MariaDB`)
- Database host, port, name, user, password
- Admin email and password
- SMTP host/port/user/password/from
- Password reset URL base
- Optional env file write path

On submit, the server:

- Validates the values
- Tests DB connectivity
- Creates tables if needed
- Bootstraps first admin account
- Optionally writes a local env file (default: `help-desk.env`)

## Admin Access

After setup, sign in at:

- `http://localhost:8080/admin/login`

After login, the admin dashboard is available at:

- `http://localhost:8080/admin`

The admin dashboard shows:

- Ticket list with status filters
- Dashboard counts
- Session info and logout button

## Build Binary

Build a production binary:

```bash
go build -o ./bin/help-desk ./cmd/server
```

Run the compiled binary:

```bash
./bin/help-desk
```

Or use Make targets:

```bash
make build
./bin/help-desk
```

Backend-specific run targets:

```bash
make run-postgres
make run-mariadb
```

Optional environment variable:

- `PORT`: custom HTTP port (default `8080`)
- `STORE_BACKEND`: `memory` (default), `postgres`, or `mariadb`
- `DB_DSN`: required when `STORE_BACKEND` is `postgres` or `mariadb`
- `ADMIN_EMAIL`: first admin user email (used only if there are no users yet)
- `ADMIN_PASSWORD`: first admin user password (min 8 chars)
- `SMTP_HOST`: SMTP server host
- `SMTP_PORT`: SMTP server port (default `587`)
- `SMTP_USER`: SMTP username (optional)
- `SMTP_PASS`: SMTP password (optional if SMTP server allows anonymous send)
- `SMTP_FROM`: sender email address for reset emails
- `SMTP_RESET_URL_BASE`: frontend URL for password reset page, example `https://helpdesk.example.com/reset-password`

## API Endpoints

### Create ticket

```http
POST /api/tickets
Content-Type: application/json

{
  "customer": "Jane Carter",
  "email": "jane@company.com",
  "subject": "VPN not connecting",
  "description": "Client cannot connect from home network.",
  "priority": "high"
}
```

### List tickets

```http
GET /api/tickets
GET /api/tickets?status=in_progress
```

### Get ticket

```http
GET /api/tickets/{id}
```

### Update ticket

```http
PATCH /api/tickets/{id}
Content-Type: application/json

{
  "status": "resolved",
  "assignee": "Alex",
  "resolution": "Reset certificate and updated VPN profile"
}
```

### Add comment

```http
POST /api/tickets/{id}/comments
Content-Type: application/json

{
  "author": "NOC Team",
  "message": "Collected logs and identified firewall mismatch",
  "internal": true
}
```

### Ticket stats

```http
GET /api/stats
```

### Login

```http
POST /api/auth/login
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "<your-password>"
}
```

### Request password reset email

```http
POST /api/auth/password-reset/request
Content-Type: application/json

{
  "email": "admin@example.com"
}
```

### Confirm password reset

```http
POST /api/auth/password-reset/confirm
Content-Type: application/json

{
  "token": "token-from-email",
  "newPassword": "<new-password>"
}
```

## Notes

- With `STORE_BACKEND=memory`, all data is volatile and reset on restart.
- With `STORE_BACKEND=postgres` or `STORE_BACKEND=mariadb`, tickets/comments persist in DB.
- User accounts and password-reset tokens are also persisted when SQL backend is used.
- SQL schema migration is handled by the app at startup (no manual migration step required).
- To stop local DB containers:

```bash
docker compose down
```

## License

MIT License

Copyright (c) 2026 QB Networks

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
