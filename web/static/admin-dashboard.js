const adminEmail = document.getElementById("admin-email");
const adminStatus = document.getElementById("admin-status");
const adminStats = document.getElementById("admin-stats");
const adminTickets = document.getElementById("admin-tickets");
const adminStatusFilter = document.getElementById("admin-status-filter");
const logoutButton = document.getElementById("logout-button");
const adminUpdateForm = document.getElementById("admin-update-ticket-form");
const adminUpdateMessage = document.getElementById("admin-update-message");
const adminCommentForm = document.getElementById("admin-comment-form");
const adminCommentMessage = document.getElementById("admin-comment-message");
const adminUpdateAssigneesDatalist = document.getElementById("admin-update-assignees-datalist");
const pendingApprovalsSection = document.getElementById("pending-approvals-section");
const pendingApprovalsList = document.getElementById("pending-approvals-list");
const pendingCount = document.getElementById("pending-count");
const pendingRefresh = document.getElementById("pending-refresh");
const approvedApplicationsList = document.getElementById("approved-applications-list");
const approvedCount = document.getElementById("approved-count");
const approvedRefresh = document.getElementById("approved-refresh");
const rejectedApplicationsList = document.getElementById("rejected-applications-list");
const rejectedCount = document.getElementById("rejected-count");
const rejectedRefresh = document.getElementById("rejected-refresh");
const approvedDeleteAll = document.getElementById("approved-delete-all");
const rejectedDeleteAll = document.getElementById("rejected-delete-all");

let currentAdminEmail = "";
let adminEmails = [];
let pendingUsers = [];
let approvedUsers = [];
let rejectedUsers = [];

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

function escapeHTML(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function isLikelyEmail(value) {
  const v = String(value || "").trim();
  const at = v.indexOf("@");
  if (at <= 0 || at === v.length - 1) return false;
  if (v.indexOf("@", at + 1) !== -1) return false;
  return v.slice(at + 1).includes(".");
}

function renderStats(stats) {
  adminStats.innerHTML = "";
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
    adminStats.appendChild(card);
  });
}

function renderTickets(tickets) {
  const safeTickets = Array.isArray(tickets) ? tickets : [];
  adminTickets.innerHTML = "";

  if (!safeTickets.length) {
    adminTickets.innerHTML = '<p class="meta">No tickets found for this filter.</p>';
    return;
  }

  safeTickets.forEach((ticket) => {
    const comments = Array.isArray(ticket.comments) ? ticket.comments : [];
    const assignees = Array.isArray(ticket.assignees) ? ticket.assignees.slice() : [];
    const card = document.createElement("article");
    card.className = "ticket";
    const safeSubject = escapeHTML(ticket.subject);
    const safeDescription = escapeHTML(ticket.description);
    const safeCustomer = escapeHTML(ticket.customer);
    const safeEmail = escapeHTML(ticket.email);
    const safeResolution = escapeHTML(ticket.resolution);
    const safePriority = escapeHTML(ticket.priority);

    const chips = assignees
      .map(
        (a) => `
          <span class="assignee-chip">
            <span class="assignee-chip-email">${escapeHTML(a)}</span>
            <button type="button" class="assignee-remove" data-ticket-id="${ticket.id}" data-email="${escapeHTML(a)}" aria-label="Remove ${escapeHTML(a)}">&times;</button>
          </span>`
      )
      .join("");

    const isClosed = ticket.status === "closed";
    const assigneeInputValue = escapeHTML(currentAdminEmail);
    const datalistId = `admin-list-${ticket.id}`;
    const datalistOptions = adminEmails
      .map((email) => `<option value="${escapeHTML(email)}"></option>`)
      .join("");
    const addForm = isClosed
      ? `<p class="meta">Ticket is closed; assignees cannot be changed.</p>`
      : `
        <form class="assignee-add" data-ticket-id="${ticket.id}">
          <input type="email" name="email" list="${datalistId}" placeholder="admin@example.com" value="${assigneeInputValue}" autocomplete="off" title="Pick an existing admin or type an email" />
          <datalist id="${datalistId}">${datalistOptions}</datalist>
          <button type="submit">Add assignee</button>
          <span class="assignee-error" data-for="${ticket.id}"></span>
        </form>`;

    card.innerHTML = `
      <div class="ticket-top">
        <strong>${safeSubject}</strong>
        <span class="ticket-id">#${ticket.id}</span>
      </div>
      <div>
        <span class="badge ${safePriority}">${safePriority.toUpperCase()}</span>
        <span class="badge">${statusLabel(ticket.status)}</span>
      </div>
      <p>${safeDescription}</p>
      <p class="meta">${safeCustomer} · ${safeEmail}</p>
      <div class="assignee-row">
        <span class="meta assignee-label">Assignees:</span>
        <div class="assignee-chips" data-ticket-id="${ticket.id}">${chips || '<span class="meta">Unassigned</span>'}</div>
      </div>
      ${ticket.resolution ? `<p class="meta">Resolution: ${safeResolution}</p>` : ""}
      ${comments.length ? `<p class="meta">Comments: ${comments.length}</p>` : ""}
      <div class="ticket-actions">${addForm}</div>
    `;
    adminTickets.appendChild(card);
  });

  adminTickets.querySelectorAll(".assignee-remove").forEach((button) => {
    button.addEventListener("click", () => {
      const ticketId = button.getAttribute("data-ticket-id");
      const email = button.getAttribute("data-email");
      if (ticketId && email) {
        removeAssignee(ticketId, email, button);
      }
    });
  });

  adminTickets.querySelectorAll(".assignee-add").forEach((form) => {
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      const ticketId = form.getAttribute("data-ticket-id");
      const input = form.querySelector('input[name="email"]');
      const errorEl = form.querySelector(".assignee-error");
      if (ticketId && input) {
        addAssignee(ticketId, input, errorEl, form);
      }
    });
  });
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    credentials: "same-origin",
    ...options,
  });

  const contentType = res.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await res.json() : null;

  if (!res.ok) {
    throw new Error(payload?.error || `Request failed (${res.status})`);
  }

  return payload;
}

async function patchAssignees(ticketId, assignees) {
  return api(`/api/tickets/${ticketId}`, {
    method: "PATCH",
    body: JSON.stringify({ assignees }),
  });
}

async function loadCurrentAssignees(ticketId) {
  const ticket = await api(`/api/tickets/${ticketId}`);
  return Array.isArray(ticket.assignees) ? ticket.assignees.slice() : [];
}

async function addAssignee(ticketId, input, errorEl, form) {
  const submit = form.querySelector("button[type=submit]");
  errorEl.textContent = "";
  const typedEmail = String(input.value || "").trim().toLowerCase();
  const fallbackEmail = String(currentAdminEmail || "").trim().toLowerCase();
  const email = typedEmail || fallbackEmail;

  if (!email) {
    submit.disabled = true;
    try {
      const result = await api(`/api/tickets/${ticketId}/notify`, { method: "POST" });
      adminStatus.textContent = result && result.message
        ? `${result.message} (to: ${result.to || "default"})`
        : "Notification email sent.";
      input.value = "";
      await refreshBoard();
    } catch (err) {
      errorEl.textContent = err.message;
    } finally {
      submit.disabled = false;
    }
    return;
  }

  if (!isLikelyEmail(email)) {
    errorEl.textContent = "Enter a valid email address.";
    return;
  }
  submit.disabled = true;
  try {
    const current = await loadCurrentAssignees(ticketId);
    if (current.includes(email)) {
      errorEl.textContent = "Already assigned.";
      return;
    }
    const next = current.concat([email]);
    await patchAssignees(ticketId, next);
    input.value = currentAdminEmail;
    adminStatus.textContent = `Assigned ${email} to ticket #${ticketId}.`;
    await refreshBoard();
  } catch (err) {
    errorEl.textContent = err.message;
  } finally {
    submit.disabled = false;
  }
}

async function removeAssignee(ticketId, email, button) {
  button.disabled = true;
  try {
    const current = await loadCurrentAssignees(ticketId);
    const next = current.filter((e) => e.toLowerCase() !== email.toLowerCase());
    if (next.length === current.length) {
      adminStatus.textContent = "Assignee already removed.";
      return;
    }
    await patchAssignees(ticketId, next);
    adminStatus.textContent = `Removed ${email} from ticket #${ticketId}.`;
    await refreshBoard();
  } catch (err) {
    button.disabled = false;
    adminStatus.textContent = err.message;
  }
}

async function refreshBoard() {
  const status = adminStatusFilter.value;
  const query = status ? `?status=${encodeURIComponent(status)}` : "";
  const [me, tickets, stats, admins] = await Promise.all([
    api("/api/auth/me"),
    api(`/api/tickets${query}`),
    api("/api/stats"),
    api("/api/admins"),
  ]);

  currentAdminEmail = String(me.email || "").trim();
  adminEmails = Array.isArray(admins && admins.emails) ? admins.emails : [];
  adminEmail.textContent = `Signed in as ${currentAdminEmail || "unknown"}`;
  adminStatus.textContent = "Tickets loaded successfully.";
  renderTickets(tickets);
  renderStats(stats);
  renderAdminUpdateAssigneesDatalist();
  prefillAdminAuthor();
  await Promise.all([refreshPendingApprovals(), refreshApprovedApplications(), refreshRejectedApplications()]);
}

async function refreshPendingApprovals() {
  if (!pendingApprovalsSection || !pendingApprovalsList || !pendingCount) return;
  try {
    const data = await api("/api/admin/pending");
    pendingUsers = Array.isArray(data && data.pending) ? data.pending : [];
  } catch (error) {
    pendingUsers = [];
    if (pendingApprovalsList) {
      pendingApprovalsList.innerHTML = `<p class="empty error">${escapeHTML(error.message || "Unable to load pending accounts.")}</p>`;
    }
    pendingCount.textContent = "0";
    return;
  }
  renderPendingApprovals();
}

function renderPendingApprovals() {
  if (!pendingApprovalsList || !pendingCount) return;
  pendingCount.textContent = String(pendingUsers.length);
  if (!pendingUsers.length) {
    pendingApprovalsList.innerHTML = `<p class="empty">No pending signups. Public registration is closed.</p>`;
    return;
  }
  pendingApprovalsList.innerHTML = pendingUsers
    .map((user) => {
      const email = escapeHTML(user.email || "");
      const created = user.createdAt ? new Date(user.createdAt).toLocaleString() : "";
      return `
        <div class="pending-user" data-email="${email}">
          <div class="pending-user-info">
            <strong>${email}</strong>
            <span class="meta">${escapeHTML(created)}</span>
          </div>
          <div class="pending-user-actions">
            <button class="primary-button pending-approve" type="button" data-email="${email}">Approve</button>
            <button class="secondary-button pending-reject" type="button" data-email="${email}">Reject</button>
          </div>
        </div>
      `;
    })
    .join("");
  pendingApprovalsList.querySelectorAll(".pending-approve").forEach((btn) => {
    btn.addEventListener("click", () => decidePending(btn.dataset.email, "approve"));
  });
  pendingApprovalsList.querySelectorAll(".pending-reject").forEach((btn) => {
    btn.addEventListener("click", () => decidePending(btn.dataset.email, "reject"));
  });
}

async function decidePending(email, action) {
  const targetEmail = String(email || "").trim();
  if (!targetEmail) return;
  const verb = action === "approve" ? "Approve" : "Reject";
  if (!window.confirm(`${verb} admin signup for ${targetEmail}?`)) return;
  try {
    await api(`/api/admin/${action}`, {
      method: "POST",
      body: JSON.stringify({ email: targetEmail }),
    });
    await Promise.all([refreshPendingApprovals(), refreshApprovedApplications(), refreshRejectedApplications()]);
  } catch (error) {
    window.alert(error.message || `Unable to ${action} account.`);
  }
}

async function deleteUsers(emails, confirmMessage) {
  const list = Array.isArray(emails) ? emails.filter(Boolean) : [];
  if (!list.length) return;
  if (!window.confirm(confirmMessage)) return;
  try {
    const result = await api("/api/admin/delete", {
      method: "POST",
      body: JSON.stringify({ emails: list }),
    });
    const deleted = Number(result && result.deleted) || 0;
    window.alert(`${deleted} account(s) deleted.`);
    await Promise.all([refreshApprovedApplications(), refreshRejectedApplications()]);
  } catch (error) {
    window.alert(error.message || "Delete failed.");
  }
}

async function deleteOneUser(email, kind) {
  await deleteUsers(
    [email],
    `Permanently delete ${kind} account ${email}? This cannot be undone.`,
  );
}

async function refreshApprovedApplications() {
  if (!approvedApplicationsList || !approvedCount) return;
  try {
    const data = await api("/api/admin/approved");
    approvedUsers = Array.isArray(data && data.users) ? data.users : [];
  } catch (error) {
    approvedUsers = [];
    approvedApplicationsList.innerHTML = `<p class="empty error">${escapeHTML(error.message || "Unable to load approved accounts.")}</p>`;
    approvedCount.textContent = "0";
    return;
  }
  renderApprovedApplications();
}

function renderApprovedApplications() {
  if (!approvedApplicationsList || !approvedCount) return;
  approvedCount.textContent = String(approvedUsers.length);
  if (!approvedUsers.length) {
    approvedApplicationsList.innerHTML = `<p class="empty">No accepted applications yet.</p>`;
    return;
  }
  approvedApplicationsList.innerHTML = approvedUsers
    .map((user) => applicationRow(user, "approved"))
    .join("");
  approvedApplicationsList.querySelectorAll(".application-delete").forEach((btn) => {
    btn.addEventListener("click", () => deleteOneUser(btn.dataset.email, "approved"));
  });
}

async function refreshRejectedApplications() {
  if (!rejectedApplicationsList || !rejectedCount) return;
  try {
    const data = await api("/api/admin/rejected");
    rejectedUsers = Array.isArray(data && data.users) ? data.users : [];
  } catch (error) {
    rejectedUsers = [];
    rejectedApplicationsList.innerHTML = `<p class="empty error">${escapeHTML(error.message || "Unable to load rejected accounts.")}</p>`;
    rejectedCount.textContent = "0";
    return;
  }
  renderRejectedApplications();
}

function renderRejectedApplications() {
  if (!rejectedApplicationsList || !rejectedCount) return;
  rejectedCount.textContent = String(rejectedUsers.length);
  if (!rejectedUsers.length) {
    rejectedApplicationsList.innerHTML = `<p class="empty">No rejected applications.</p>`;
    return;
  }
  rejectedApplicationsList.innerHTML = rejectedUsers
    .map((user) => applicationRow(user, "rejected"))
    .join("");
  rejectedApplicationsList.querySelectorAll(".application-delete").forEach((btn) => {
    btn.addEventListener("click", () => deleteOneUser(btn.dataset.email, "rejected"));
  });
}

function applicationRow(user, kind) {
  const email = escapeHTML(user.email || "");
  const created = user.createdAt ? new Date(user.createdAt).toLocaleString() : "";
  const canDelete = kind === "approved"
    ? email.toLowerCase() !== String(currentAdminEmail || "").toLowerCase()
    : true;
  return `
    <div class="pending-user application-${kind}" data-email="${email}">
      <div class="pending-user-info">
        <strong>${email}</strong>
        <span class="meta">${escapeHTML(created)}</span>
      </div>
      <div class="pending-user-actions">
        <span class="application-status application-status-${kind}">${kind}</span>
        ${canDelete ? `<button class="danger-button application-delete" type="button" data-email="${email}" data-kind="${kind}">Delete</button>` : ""}
      </div>
    </div>
  `;
}

function renderAdminUpdateAssigneesDatalist() {
  if (!adminUpdateAssigneesDatalist) return;
  adminUpdateAssigneesDatalist.innerHTML = adminEmails
    .map((email) => `<option value="${escapeHTML(email)}"></option>`)
    .join("");
}

function prefillAdminAuthor() {
  if (!adminCommentForm || !currentAdminEmail) return;
  const authorInput = adminCommentForm.querySelector('input[name="author"]');
  if (authorInput && !authorInput.value) {
    authorInput.value = currentAdminEmail;
  }
}

function setAdminActionMessage(target, text, isError = false) {
  if (!target) return;
  target.textContent = text || "";
  target.classList.toggle("error", isError);
  target.classList.toggle("ok", !isError && Boolean(text));
}

if (pendingRefresh) {
  pendingRefresh.addEventListener("click", () => {
    refreshPendingApprovals().catch(() => {});
  });
}
if (approvedRefresh) {
  approvedRefresh.addEventListener("click", () => {
    refreshApprovedApplications().catch(() => {});
  });
}
if (rejectedRefresh) {
  rejectedRefresh.addEventListener("click", () => {
    refreshRejectedApplications().catch(() => {});
  });
}
if (approvedDeleteAll) {
  approvedDeleteAll.addEventListener("click", () => {
    const others = approvedUsers
      .map((u) => String(u.email || "").trim())
      .filter((e) => e && e.toLowerCase() !== String(currentAdminEmail || "").toLowerCase());
    deleteUsers(
      others,
      `Permanently delete ${others.length} approved admin account(s) (everyone except you)? This cannot be undone.`,
    );
  });
}
if (rejectedDeleteAll) {
  rejectedDeleteAll.addEventListener("click", () => {
    const all = rejectedUsers.map((u) => String(u.email || "").trim()).filter(Boolean);
    deleteUsers(
      all,
      `Permanently delete ${all.length} rejected application(s)? This cannot be undone.`,
    );
  });
}

adminUpdateForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setAdminActionMessage(adminUpdateMessage, "");

  const formData = new FormData(adminUpdateForm);
  const ticketId = formData.get("ticketId");
  if (!ticketId) {
    setAdminActionMessage(adminUpdateMessage, "Ticket ID is required.", true);
    return;
  }

  const body = {
    assignees: String(formData.get("assignees") || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean),
    status: String(formData.get("status") || ""),
    resolution: String(formData.get("resolution") || ""),
  };

  const button = adminUpdateForm.querySelector("button[type=submit]");
  button.disabled = true;
  try {
    await api(`/api/tickets/${ticketId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
    setAdminActionMessage(adminUpdateMessage, `Ticket #${ticketId} updated.`);
    await refreshBoard();
  } catch (error) {
    setAdminActionMessage(adminUpdateMessage, error.message, true);
  } finally {
    button.disabled = false;
  }
});

adminCommentForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setAdminActionMessage(adminCommentMessage, "");

  const formData = new FormData(adminCommentForm);
  const ticketId = formData.get("ticketId");
  if (!ticketId) {
    setAdminActionMessage(adminCommentMessage, "Ticket ID is required.", true);
    return;
  }

  const author = String(formData.get("author") || "").trim();
  const message = String(formData.get("message") || "").trim();
  if (!author || !message) {
    setAdminActionMessage(adminCommentMessage, "Author and message are required.", true);
    return;
  }

  const body = {
    author,
    message,
    internal: formData.get("internal") === "on",
  };

  const button = adminCommentForm.querySelector("button[type=submit]");
  button.disabled = true;
  try {
    await api(`/api/tickets/${ticketId}/comments`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    setAdminActionMessage(adminCommentMessage, `Comment posted to ticket #${ticketId}.`);
    adminCommentForm.querySelector('input[name="message"]').value = "";
    await refreshBoard();
  } catch (error) {
    setAdminActionMessage(adminCommentMessage, error.message, true);
  } finally {
    button.disabled = false;
  }
});

logoutButton.addEventListener("click", async () => {
  try {
    await api("/api/auth/logout", { method: "POST" });
    window.location.replace("/admin/login");
  } catch (error) {
    adminStatus.textContent = error.message;
  }
});

adminStatusFilter.addEventListener("change", () => {
  refreshBoard().catch((error) => {
    adminStatus.textContent = error.message;
  });
});

refreshBoard().catch((error) => {
  if (String(error.message || "").includes("not authenticated")) {
    window.location.replace("/admin/login");
    return;
  }
  adminStatus.textContent = error.message;
});
