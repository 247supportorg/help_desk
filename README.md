# FactoryX Help Desk

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

- Backend: Go (`net/http`, in-memory store with `sync.RWMutex`)
- Frontend: HTML, CSS, Vanilla JavaScript

## Run Locally

```bash
go run ./cmd/server
```

Server starts at `http://localhost:8080`.

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

Optional environment variable:

- `PORT`: custom HTTP port (default `8080`)

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

## Notes

- Data is stored in memory. Restarting the server clears tickets.
- To persist data, you can replace the in-memory store with PostgreSQL or MySQL.

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
