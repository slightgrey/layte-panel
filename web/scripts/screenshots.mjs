// Takes the README screenshots: `npm run screenshots` (from web/).
//
// It builds the UI and the Go binary, then runs a private copy of Layte Panel
// in dev mode (fake demo server: example.com sites, fake processes) in a
// temporary folder on a free port, so your own `.dev/` and a running dev
// server are never touched. A throwaway admin signs in (2FA codes are
// computed here), a demo WordPress site with HTTPS is created, and the pages
// are captured with your installed Chrome. Output: docs/screenshots/*.png.

import { spawn, spawnSync } from "node:child_process";
import { createHmac, randomBytes } from "node:crypto";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const web = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repo = resolve(web, "..");
const out = join(repo, "docs", "screenshots");
const exe = process.platform === "win32" ? "layte.exe" : "layte";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function run(cmd, args, cwd) {
  // npm is a .cmd file on Windows and needs a shell there; go must not get one
  // (the shell would split -ldflags at its space).
  const r = spawnSync(cmd, args, { cwd, stdio: "inherit", shell: process.platform === "win32" && cmd === "npm" });
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(" ")} failed`);
}

function freePort() {
  return new Promise((ok) => {
    const s = createServer().listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => ok(port));
    });
  });
}

// RFC 6238 TOTP (SHA-1, 30 s, 6 digits), the same as authenticator apps.
function totp(secret, at = Date.now()) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const ch of secret.replace(/=+$/, "").toUpperCase()) bits += alphabet.indexOf(ch).toString(2).padStart(5, "0");
  const key = Buffer.from(bits.match(/.{8}/g).map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30000)));
  const h = createHmac("sha1", key).update(counter).digest();
  const o = h[h.length - 1] & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, "0");
}

// Waits until the next 30 s window, so a code is never reused (Layte rejects replays).
async function freshTOTP(secret) {
  const left = 30000 - (Date.now() % 30000);
  await sleep(left + 500);
  return totp(secret);
}

async function waitFor(check, what, ms = 60000) {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    if (await check().catch(() => false)) return;
    await sleep(300);
  }
  throw new Error(`timed out waiting for ${what}`);
}

const tmp = mkdtempSync(join(tmpdir(), "layte-shots-"));
const procs = [];
let browser;
let failLogs = () => "";

async function main() {
  console.log("Building the UI and the binary…");
  run("npm", ["run", "build"], web);
  // Stamp the real version (e.g. v0.1.16) so the sidebar footer doesn't say "dev".
  const version = spawnSync("git", ["describe", "--tags", "--abbrev=0"], { cwd: repo, encoding: "utf8" }).stdout.trim() || "dev";
  run("go", ["build", "-ldflags", `-X main.version=${version.replace(/^v/, "")}`, "-o", join(tmp, exe), "./cmd/layte"], repo);
  const bin = join(tmp, exe);

  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const env = {
    ...process.env,
    LAYTE_LISTEN: `127.0.0.1:${port}`,
    LAYTE_DATA_DIR: join(tmp, "data"),
    LAYTE_AGENT_SOCKET: join(tmp, "agent.sock"),
    LAYTE_SECRET_KEY_FILE: join(tmp, "secret.key"),
  };
  const logs = [];
  const start = (args) => {
    const p = spawn(bin, args, { cwd: tmp, env, stdio: ["ignore", "ignore", "pipe"] });
    const log = { name: args[0], text: "" };
    logs.push(log);
    p.stderr.on("data", (d) => (log.text += d));
    procs.push(p);
    return log;
  };
  failLogs = () => logs.map((l) => `--- ${l.name}:\n${l.text.slice(-2000)}`).join("\n");

  console.log("Starting a private dev agent and panel on port", port);
  // Wait for its log line: on Windows a unix socket file isn't visible to fs.existsSync.
  const agentLog = start(["agent", "--dev"]);
  await waitFor(async () => agentLog.text.includes("agent listening"), "the agent");

  // Throwaway admin: random password, 2FA secret read from the enrolment output.
  const password = randomBytes(18).toString("base64url");
  const secret = await new Promise((ok, fail) => {
    const p = spawn(bin, ["admin", "create", "--dev", "--username", "demo"], { cwd: tmp, env, stdio: ["pipe", "ignore", "pipe"] });
    let err = "";
    let sec = null;
    p.stdin.write(`${password}\n${password}\n`);
    p.stderr.on("data", (d) => {
      err += d;
      const m = err.match(/Secret:\s+([A-Z2-7]+)/);
      if (m && !sec) {
        sec = m[1];
        p.stdin.write(`${totp(sec)}\n`);
      }
    });
    p.on("exit", (code) => (code === 0 && sec ? ok(sec) : fail(new Error("admin create failed:\n" + err))));
  });

  start(["serve", "--dev"]);
  await waitFor(async () => (await fetch(`${base}/api/v1/health`)).ok, "the panel");

  browser = await chromium.launch({ channel: "chrome", headless: true });
  const ctx = await browser.newContext({ baseURL: base, viewport: { width: 1440, height: 900 }, colorScheme: "light" });
  const page = await ctx.newPage();
  const pageErrors = [];
  page.on("pageerror", (e) => pageErrors.push(`page error: ${e.message}`));
  page.on("console", (m) => m.type() === "error" && pageErrors.push(`console: ${m.text()}`));
  const prevLogs = failLogs;
  failLogs = () => [prevLogs(), ...pageErrors].join("\n");

  console.log("Signing in…");
  await page.goto("/login");
  await page.fill("#username", "demo");
  await page.fill("#password", password);
  await page.click('button[type="submit"]');
  await page.waitForURL("**/login/2fa");
  await page.fill("#code", await freshTOTP(secret));
  await page.click('button[type="submit"]');
  await page.waitForURL(base + "/");

  // Demo data through the API (same session; CSRF token from /auth/me).
  const csrf = (await (await ctx.request.get("/api/v1/auth/me")).json()).csrf_token;
  const call = async (method, path, data) => {
    const r = await ctx.request.fetch(path, { method, data, headers: { "X-CSRF-Token": csrf } });
    if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`);
    return r.status() === 204 ? null : r.json();
  };
  const job = async (started) => {
    let j;
    await waitFor(async () => (j = await call("GET", `/api/v1/jobs/${started.job_id}`)).status !== "running", "a job", 120000);
    if (j.status !== "succeeded") throw new Error("job failed: " + j.error);
    return started;
  };
  console.log("Creating demo data…");
  await call("POST", "/api/v1/sites/scan");
  const site = await job(
    await call("POST", "/api/v1/sites", {
      domain: "demo.example.com",
      aliases: ["www.demo.example.com"],
      php_version: "8.3",
      create_db: true,
      wordpress: { title: "Demo", admin_user: "owner", admin_email: "owner@example.com" },
    }),
  );
  await job(await call("POST", `/api/v1/sites/${site.site_id}/ssl`, { email: "owner@example.com" }));
  await call("POST", `/api/v1/sites/${site.site_id}/cron`, { schedule: "*/15 * * * *", command: "php public/wp-cron.php" });

  mkdirSync(out, { recursive: true });
  const shot = async (name, path, waitText) => {
    await page.goto(path);
    await page.getByText(waitText, { exact: false }).first().waitFor({ timeout: 30000 });
    await sleep(800); // let bars and badges settle
    await page.screenshot({ path: join(out, `${name}.png`) });
    console.log("  saved", `docs/screenshots/${name}.png`);
  };

  console.log("Taking screenshots…");
  await shot("dashboard", "/", "Services");
  await shot("sites", "/sites", "demo.example.com");

  // The New site form, filled in (not submitted).
  await page.goto("/sites/new");
  await page.locator("#php option").first().waitFor({ state: "attached", timeout: 30000 });
  await page.fill("#domain", "store.example.com");
  await page.fill("#aliases", "www.store.example.com");
  const wp = page.getByLabel("Install WordPress");
  if (!(await wp.isChecked())) await wp.check();
  await page.fill("#title", "My Store");
  await page.fill("#admin_user", "owner");
  await page.fill("#admin_email", "owner@example.com");
  await page.locator("body").click({ position: { x: 5, y: 5 } }); // no focus ring / caret in the picture
  await sleep(500);
  await page.screenshot({ path: join(out, "new-site.png"), fullPage: true }); // the form is taller than the window
  console.log("  saved docs/screenshots/new-site.png");
  await shot("site-overview", `/sites/${site.site_id}`, "Size on disk");
  await shot("memory", "/system/memory", "By service and site");
  await shot("updates", "/updates", "Kept back");

  await page.evaluate(() => localStorage.setItem("layte-theme", "dark"));
  await page.emulateMedia({ colorScheme: "dark" });
  await shot("dashboard-dark", "/", "Services");
}

async function cleanup() {
  await browser?.close().catch(() => {});
  for (const p of procs) p.kill();
  await sleep(500);
  for (let i = 0; i < 5; i++) {
    try {
      rmSync(tmp, { recursive: true, force: true });
      break;
    } catch {
      await sleep(500); // Windows: files are released a moment after the processes exit
    }
  }
}

main()
  .then(cleanup)
  .then(() => console.log("Done."))
  .catch(async (e) => {
    console.error(e.message ?? e);
    console.error(failLogs());
    await cleanup();
    process.exit(1);
  });
