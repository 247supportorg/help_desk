/*
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
*/

const createForm = document.getElementById("create-ticket-form");
const createMessage = document.getElementById("create-message");
const updateForm = document.getElementById("update-ticket-form");
const updateMessage = document.getElementById("update-message");
const commentForm = document.getElementById("comment-form");
const commentMessage = document.getElementById("comment-message");
const statusFilter = document.getElementById("status-filter");
const ticketsContainer = document.getElementById("tickets");
const statsContainer = document.getElementById("stats");

function statusLabel(value) {
  switch (value) {
    case "in_progress":
      return "In Progress";
    case "resolved":
      return "Resolved";
    case "closed":
      return "Closed";
    default:
      return "Open";
  }
}

function setMessage(target, text, isError = false) {
  target.textContent = text;
  target.classList.toggle("error", isError);
  target.classList.toggle("ok", !isError && text.length > 0);
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  const contentType = res.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await res.json() : null;

  if (!res.ok) {
    throw new Error(payload?.error || `Request failed (${res.status})`);
  }

  return payload;
}

function renderStats(stats) {
  statsContainer.innerHTML = "";
  const items = [
    ["Open", stats.open],
    ["In Progress", stats.inProgress],
    ["Resolved", stats.resolved],
    ["Closed", stats.closed],
    ["Total", stats.total],
  ];

  items.forEach(([label, value]) => {
    const card = document.createElement("div");
    card.className = "stat";
    card.innerHTML = `<span class="stat-label">${label}</span><span class="stat-value">${value}</span>`;
    statsContainer.appendChild(card);
  });
}

function renderTickets(tickets) {
  ticketsContainer.innerHTML = "";
  if (!tickets.length) {
    ticketsContainer.innerHTML = '<p class="meta">No tickets found for this filter.</p>';
    return;
  }

  tickets.forEach((ticket) => {
    const card = document.createElement("article");
    card.className = "ticket";
    card.innerHTML = `
      <div class="ticket-top">
        <strong>${ticket.subject}</strong>
        <span class="ticket-id">#${ticket.id}</span>
      </div>
      <div>
        <span class="badge ${ticket.priority}">${ticket.priority.toUpperCase()}</span>
        <span class="badge">${statusLabel(ticket.status)}</span>
      </div>
      <p>${ticket.description}</p>
      <p class="meta">${ticket.customer} · ${ticket.email}</p>
      <p class="meta">Assignee: ${ticket.assignee || "Unassigned"}</p>
      ${ticket.resolution ? `<p class="meta">Resolution: ${ticket.resolution}</p>` : ""}
      ${ticket.comments.length ? `<p class="meta">Comments: ${ticket.comments.length}</p>` : ""}
    `;
    ticketsContainer.appendChild(card);
  });
}

async function refreshBoard() {
  const status = statusFilter.value;
  const query = status ? `?status=${encodeURIComponent(status)}` : "";
  const [tickets, stats] = await Promise.all([
    api(`/api/tickets${query}`),
    api("/api/stats"),
  ]);

  renderTickets(tickets);
  renderStats(stats);
}

createForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(createMessage, "");

  const formData = new FormData(createForm);
  const body = Object.fromEntries(formData.entries());

  try {
    const ticket = await api("/api/tickets", {
      method: "POST",
      body: JSON.stringify(body),
    });

    createForm.reset();
    setMessage(createMessage, `Ticket #${ticket.id} created successfully.`);
    await refreshBoard();
  } catch (err) {
    setMessage(createMessage, err.message, true);
  }
});

updateForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(updateMessage, "");

  const formData = new FormData(updateForm);
  const ticketId = formData.get("ticketId");
  if (!ticketId) {
    setMessage(updateMessage, "Ticket ID is required.", true);
    return;
  }

  const body = {
    assignee: String(formData.get("assignee") || ""),
    status: String(formData.get("status") || ""),
    resolution: String(formData.get("resolution") || ""),
  };

  try {
    await api(`/api/tickets/${ticketId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });

    setMessage(updateMessage, `Ticket #${ticketId} updated.`);
    await refreshBoard();
  } catch (err) {
    setMessage(updateMessage, err.message, true);
  }
});

commentForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(commentMessage, "");

  const formData = new FormData(commentForm);
  const ticketId = formData.get("ticketId");
  if (!ticketId) {
    setMessage(commentMessage, "Ticket ID is required.", true);
    return;
  }

  const body = {
    author: String(formData.get("author") || ""),
    message: String(formData.get("message") || ""),
    internal: formData.get("internal") === "on",
  };

  try {
    await api(`/api/tickets/${ticketId}/comments`, {
      method: "POST",
      body: JSON.stringify(body),
    });

    commentForm.reset();
    setMessage(commentMessage, `Comment posted to ticket #${ticketId}.`);
    await refreshBoard();
  } catch (err) {
    setMessage(commentMessage, err.message, true);
  }
});

statusFilter.addEventListener("change", () => {
  refreshBoard().catch((err) => {
    setMessage(updateMessage, err.message, true);
  });
});

refreshBoard().catch((err) => {
  setMessage(createMessage, `Failed to load tickets: ${err.message}`, true);
});
