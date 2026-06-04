const loginForm = document.getElementById("admin-login-form");
const loginMessage = document.getElementById("admin-login-message");
const forgotLink = document.getElementById("forgot-password-link");
const forgotCancel = document.getElementById("forgot-password-cancel");
const forgotForm = document.getElementById("forgot-password-form");
const forgotMessage = document.getElementById("forgot-password-message");
const signupLink = document.getElementById("admin-signup-link");
const signupCancel = document.getElementById("admin-signup-cancel");
const signupForm = document.getElementById("admin-signup-form");
const signupMessage = document.getElementById("admin-signup-message");

function setMessage(target, text, isError = false) {
  target.textContent = text || "";
  target.classList.toggle("error", isError);
  target.classList.toggle("ok", !isError && Boolean(text));
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

function inheritedEmail() {
  return String(loginForm.querySelector('input[name="email"]').value || "").trim();
}

function showLogin() {
  loginForm.hidden = false;
  forgotForm.hidden = true;
  signupForm.hidden = true;
  setMessage(forgotMessage, "");
  setMessage(loginMessage, "");
  setMessage(signupMessage, "");
  const link = document.querySelector(".auth-link");
  if (link) link.hidden = false;
}

function showForgot() {
  loginForm.hidden = true;
  forgotForm.hidden = false;
  signupForm.hidden = true;
  setMessage(loginMessage, "");
  setMessage(signupMessage, "");
  const link = document.querySelector(".auth-link");
  if (link) link.hidden = true;
  const emailInput = forgotForm.querySelector('input[name="email"]');
  if (emailInput) {
    emailInput.value = inheritedEmail();
    emailInput.focus();
  }
}

function showSignup() {
  loginForm.hidden = true;
  forgotForm.hidden = true;
  signupForm.hidden = false;
  setMessage(loginMessage, "");
  setMessage(forgotMessage, "");
  const link = document.querySelector(".auth-link");
  if (link) link.hidden = true;
  const emailInput = signupForm.querySelector('input[name="email"]');
  if (emailInput) {
    emailInput.value = inheritedEmail();
    emailInput.focus();
  }
}

async function bootstrap() {
  try {
    await api("/api/auth/me", { method: "GET" });
    window.location.replace("/admin");
  } catch (_) {
    // stay on login page
  }
}

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(loginMessage, "");

  const formData = new FormData(loginForm);
  const payload = {
    email: String(formData.get("email") || ""),
    password: String(formData.get("password") || ""),
  };

  try {
    await api("/api/auth/login", {
      method: "POST",
      body: JSON.stringify(payload),
      credentials: "same-origin",
    });
    setMessage(loginMessage, "Signed in successfully.");
    window.location.replace("/admin");
  } catch (error) {
    setMessage(loginMessage, error.message, true);
  }
});

forgotLink.addEventListener("click", (event) => {
  event.preventDefault();
  showForgot();
});

forgotCancel.addEventListener("click", (event) => {
  event.preventDefault();
  showLogin();
});

forgotForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(forgotMessage, "");

  const formData = new FormData(forgotForm);
  const email = String(formData.get("email") || "").trim();
  if (!email) {
    setMessage(forgotMessage, "Email is required.", true);
    return;
  }

  const button = forgotForm.querySelector("button[type=submit]");
  button.disabled = true;
  try {
    await api("/api/auth/password-reset/request", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
    setMessage(forgotMessage, "If that account exists and SMTP is configured, a reset link has been sent.");
  } catch (error) {
    setMessage(forgotMessage, error.message, true);
  } finally {
    button.disabled = false;
  }
});

signupLink.addEventListener("click", (event) => {
  event.preventDefault();
  showSignup();
});

signupCancel.addEventListener("click", (event) => {
  event.preventDefault();
  showLogin();
});

signupForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage(signupMessage, "");

  const formData = new FormData(signupForm);
  const payload = {
    email: String(formData.get("email") || "").trim(),
    password: String(formData.get("password") || ""),
    confirmPassword: String(formData.get("confirmPassword") || ""),
  };

  if (!payload.email) {
    setMessage(signupMessage, "Email is required.", true);
    return;
  }
  if (payload.password.length < 8) {
    setMessage(signupMessage, "Password must be at least 8 characters.", true);
    return;
  }
  if (payload.password !== payload.confirmPassword) {
    setMessage(signupMessage, "Passwords do not match.", true);
    return;
  }

  const button = signupForm.querySelector("button[type=submit]");
  button.disabled = true;
  try {
    await api("/api/auth/signup", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    setMessage(signupMessage, "Account created. You can sign in now.");
    setTimeout(() => {
      window.location.replace("/admin");
    }, 600);
  } catch (error) {
    setMessage(signupMessage, error.message, true);
    button.disabled = false;
  }
});

bootstrap();
