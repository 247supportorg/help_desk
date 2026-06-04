const setupForm = document.getElementById("setup-form");
const setupMessage = document.getElementById("setup-message");
const setupResult = document.getElementById("setup-result");
const backendSelect = document.getElementById("store-backend");
const dbPortInput = document.getElementById("db-port");

function setMessage(text, isError = false) {
  const safeText = text ?? "";
  setupMessage.textContent = safeText;
  setupMessage.classList.toggle("error", isError);
  setupMessage.classList.toggle("ok", !isError && safeText.length > 0);
}

function buildSetupHint(errorMessage, payload) {
  const message = String(errorMessage || "");
  const backend = String(payload?.storeBackend || "").toLowerCase();
  const mariadbComposeHint = "docker compose up -d mariadb (veya docker-compose up -d mariadb)";
  const postgresComposeHint = "docker compose up -d postgres (veya docker-compose up -d postgres)";
  const mariadbPortHint = "3306 doluysa: sudo env MARIADB_HOST_PORT=3307 docker-compose up -d mariadb";
  const postgresPortHint = "5432 doluysa: sudo env POSTGRES_HOST_PORT=5433 docker-compose up -d postgres";
  const dockerPermissionHint = [
    "Docker socket izni yok gibi görünüyor.",
    "Geçici çözüm: sudo docker-compose up -d mariadb",
    "Kalıcı çözüm: sudo usermod -aG docker $USER && oturumu kapatıp açın",
  ].join("\n");
  const authFailedHint = [
    "Veritabanı parolası doğrulanamadı.",
    "Eğer konteyneri daha önce farklı parolayla başlattıysanız volume'ü silip yeniden oluşturun:",
    "sudo docker-compose down -v",
    "Sonra doğru parola ile tekrar başlatın.",
  ].join("\n");
  const composeV1CleanupHint = [
    "docker-compose v1 eski konteyner metadata'sında takılmış olabilir.",
    "Temiz başlatma: sudo docker-compose down -v",
    "Container adı takılıysa: sudo docker rm -f helpdesk-mariadb",
    "Sonra tekrar deneyin: sudo docker-compose up -d mariadb",
  ].join("\n");

  if (message.includes("connection refused") && message.includes(":3306")) {
    return [
      "MariaDB bağlantısı başarısız: servis çalışmıyor olabilir.",
      `Çalıştırın: ${mariadbComposeHint}`,
      mariadbPortHint,
      "Sistem servisi varsa: sudo systemctl start mariadb",
      "Docker yetkisi yoksa: sudo docker-compose up -d mariadb",
    ].join("\n");
  }

  if (message.includes("connection refused") && message.includes(":5432")) {
    return [
      "PostgreSQL bağlantısı başarısız: servis çalışmıyor olabilir.",
      `Çalıştırın: ${postgresComposeHint}`,
      postgresPortHint,
      "Sistem servisi varsa: sudo systemctl start postgresql",
      "Docker yetkisi yoksa: sudo docker-compose up -d postgres",
    ].join("\n");
  }

  if (message.includes("Permission denied") && message.includes("docker")) {
    return dockerPermissionHint;
  }

  if (message.includes("ContainerConfig")) {
    return composeV1CleanupHint;
  }

  if (message.includes("password authentication failed")) {
    if (backend === "postgres") {
      return [
        "PostgreSQL kimlik doğrulaması başarısız: kullanıcı adı veya parola yanlış olabilir.",
        authFailedHint,
        "PostgreSQL için DSN/parola ile container env değerinin aynı olduğundan emin olun.",
      ].join("\n");
    }
    if (backend === "mariadb") {
      return [
        "MariaDB kimlik doğrulaması başarısız: kullanıcı adı veya parola yanlış olabilir.",
        authFailedHint,
        "MariaDB için DB_DSN ile container env değerinin aynı olduğundan emin olun.",
      ].join("\n");
    }
  }

  if (message.includes("db ping failed")) {
    if (backend === "mariadb") {
      return `MariaDB servisini başlatıp tekrar deneyin: ${mariadbComposeHint}\n${mariadbPortHint}`;
    }
    if (backend === "postgres") {
      return `PostgreSQL servisini başlatıp tekrar deneyin: ${postgresComposeHint}\n${postgresPortHint}`;
    }
  }

  return "Setup failed. Check your values and try again.";
}

async function setupAPI(payload) {
  const res = await fetch("/api/setup/apply", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });

  const contentType = res.headers.get("content-type") || "";
  const body = contentType.includes("application/json") ? await res.json() : null;

  if (!res.ok) {
    throw new Error(body?.error || `Setup failed (${res.status})`);
  }

  return body;
}

function updateDefaultPort() {
  if (backendSelect.value === "postgres") {
    dbPortInput.value = "5432";
    return;
  }
  dbPortInput.value = "3306";
}

backendSelect.addEventListener("change", updateDefaultPort);

setupForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setMessage("");

  const formData = new FormData(setupForm);
  const skipEncrypted = formData.get("skipEncryptedSnapshot") === "on";
  const encryptionPassphrase = String(formData.get("encryptionPassphrase") || "");

  if (!skipEncrypted && encryptionPassphrase.length < 8) {
    setMessage("Encryption passphrase must be at least 8 characters, or check 'Skip encrypted snapshot'.", true);
    return;
  }

  const payload = {
    appPort: String(formData.get("appPort") || ""),
    envPath: String(formData.get("envPath") || ""),
    writeEnvFile: formData.get("writeEnvFile") === "on",
    storeBackend: String(formData.get("storeBackend") || "postgres"),
    dbHost: String(formData.get("dbHost") || ""),
    dbPort: String(formData.get("dbPort") || ""),
    dbName: String(formData.get("dbName") || ""),
    dbUser: String(formData.get("dbUser") || ""),
    dbPassword: String(formData.get("dbPassword") || ""),
    adminEmail: String(formData.get("adminEmail") || ""),
    adminPassword: String(formData.get("adminPassword") || ""),
    smtpHost: String(formData.get("smtpHost") || ""),
    smtpPort: String(formData.get("smtpPort") || ""),
    smtpUser: String(formData.get("smtpUser") || ""),
    smtpPass: String(formData.get("smtpPass") || ""),
    smtpFrom: String(formData.get("smtpFrom") || ""),
    smtpResetURLBase: String(formData.get("smtpResetURLBase") || ""),
    setupEncPath: String(formData.get("setupEncPath") || ""),
    encryptionPassphrase: skipEncrypted ? "" : encryptionPassphrase,
  };

  try {
    const result = await setupAPI(payload);
    const lines = [
      `Message            : ${result.message}`,
      `Env File           : ${result.envPath}`,
      `Backend            : ${payload.storeBackend}`,
      `DSN                : ${result.dsn}`,
    ];
    if (result.setupEncPath) {
      lines.push(
        `Encrypted Snapshot : ${result.setupEncPath}`,
        "",
        "To decrypt later:",
        `  ${location.pathname.endsWith("/") ? "" : "./"}help-desk decrypt-setup ${result.setupEncPath} --passphrase '***'`,
        "",
        "The encrypted file uses AES-256-GCM with a scrypt-derived key; without the passphrase the file is unreadable.",
      );
    } else {
      lines.push("", "Encrypted snapshot was skipped (no snapshot file written).");
    }
    lines.push(
      "",
      "Run command:",
      `  PORT=${payload.appPort || "8080"} STORE_BACKEND=${payload.storeBackend} DB_DSN='<dsn>' go run ./cmd/server`,
    );
    setMessage("Setup applied successfully.");
    setupResult.textContent = lines.join("\n");
  } catch (error) {
    setMessage(`${error.message}\n\n${buildSetupHint(error.message, payload)}`, true);
    setupResult.textContent = "Setup failed. Check your values and try again.";
  }
});
