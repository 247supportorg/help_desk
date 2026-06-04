const form = document.getElementById("reset-password-form");
const message = document.getElementById("reset-password-message");
const errorEl = document.getElementById("reset-error");
const subtext = document.getElementById("reset-subtext");

function setMessage(target, text, isError = false) {
  target.textContent = text || "";
  target.classList.toggle("error", isError);
  target.classList.toggle("ok", !isError && Boolean(text));
}

function getTokenFromUrl() {
  const params = new URLSearchParams(window.location.search);
  return String(params.get("token") || "").trim();
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

const token = getTokenFromUrl();
if (!token) {
  errorEl.hidden = false;
  errorEl.textContent = "Reset token missing. Open the link from your reset email.";
  subtext.textContent = "No reset token detected in the URL.";
} else {
  form.hidden = false;
  form.querySelector('input[name="newPassword"]').focus();
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(message, "");
  errorEl.hidden = true;

  const formData = new FormData(form);
  const newPassword = String(formData.get("newPassword") || "");
  const confirmPassword = String(formData.get("confirmPassword") || "");

  if (newPassword.length < 8) {
    setMessage(message, "Password must be at least 8 characters.", true);
    return;
  }
  if (newPassword !== confirmPassword) {
    setMessage(message, "Passwords do not match.", true);
    return;
  }

  const button = form.querySelector("button[type=submit]");
  button.disabled = true;
  try {
    await api("/api/auth/password-reset/confirm", {
      method: "POST",
      body: JSON.stringify({ token, newPassword }),
    });
    setMessage(message, "Password updated. Redirecting to sign in...");
    form.querySelectorAll("input").forEach((input) => (input.disabled = true));
    setTimeout(() => {
      window.location.replace("/admin/login");
    }, 1500);
  } catch (err) {
    setMessage(message, err.message, true);
    button.disabled = false;
  }
});
