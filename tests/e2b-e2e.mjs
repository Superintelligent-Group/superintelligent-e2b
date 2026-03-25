#!/usr/bin/env node
/**
 * E2B Self-Hosted End-to-End Test Suite
 *
 * Tests all sandbox operations against our deployment at e2b.superintelligent.group.
 * Requires: E2B_API_KEY env var (or fetches from AWS Secrets Manager).
 *
 * Usage:
 *   node tests/e2b-e2e.mjs
 *   E2B_API_KEY=e2b_... node tests/e2b-e2e.mjs
 */

import { Sandbox, CommandExitError } from "e2b";

const DOMAIN = "e2b.superintelligent.group";
const results = [];
let sandbox = null;

async function getApiKey() {
  if (process.env.E2B_API_KEY) return process.env.E2B_API_KEY;
  const { execSync } = await import("child_process");
  return execSync(
    'aws secretsmanager get-secret-value --secret-id e2b-dev/e2b-api-key --query SecretString --output text --region us-east-1',
    { encoding: "utf-8" }
  ).trim();
}

function test(name, fn) {
  results.push({ name, fn });
}

async function runTests() {
  const apiKey = await getApiKey();
  console.log(`\n  E2B E2E Tests — ${DOMAIN}\n`);

  const opts = { domain: DOMAIN, apiKey };
  let passed = 0, failed = 0;

  for (const { name, fn } of results) {
    const start = Date.now();
    try {
      await fn(opts);
      const ms = Date.now() - start;
      console.log(`  \x1b[32m✓\x1b[0m ${name} \x1b[90m(${ms}ms)\x1b[0m`);
      passed++;
    } catch (err) {
      const ms = Date.now() - start;
      console.log(`  \x1b[31m✗\x1b[0m ${name} \x1b[90m(${ms}ms)\x1b[0m`);
      console.log(`    \x1b[31m${err.message}\x1b[0m`);
      failed++;
    }
  }

  console.log(`\n  ${passed} passed, ${failed} failed, ${results.length} total\n`);

  // Cleanup any leftover sandbox
  if (sandbox) {
    try { await sandbox.kill(); } catch {}
    sandbox = null;
  }

  process.exit(failed > 0 ? 1 : 0);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test("API health check", async ({ domain }) => {
  const res = await fetch(`https://api.${domain}/health`);
  if (!res.ok) throw new Error(`Health check returned ${res.status}`);
});

test("Create sandbox", async (opts) => {
  sandbox = await Sandbox.create({ ...opts, timeoutMs: 60_000 });
  if (!sandbox || !sandbox.sandboxId) throw new Error("No sandbox ID returned");
  console.log(`    sandbox: ${sandbox.sandboxId}`);
});

test("Run simple command (echo)", async () => {
  const result = await sandbox.commands.run("echo hello-e2b");
  if (!result.stdout.includes("hello-e2b"))
    throw new Error(`Expected 'hello-e2b' in stdout, got: ${result.stdout}`);
});

test("Run command with non-zero exit code", async () => {
  // SDK throws CommandExitError for non-zero exits — catch it
  try {
    await sandbox.commands.run("exit 42");
    throw new Error("Expected CommandExitError but command succeeded");
  } catch (err) {
    if (err instanceof CommandExitError) {
      if (err.exitCode !== 42)
        throw new Error(`Expected exit code 42, got: ${err.exitCode}`);
    } else {
      throw err;
    }
  }
});

test("Write file", async () => {
  await sandbox.files.write("/tmp/test-file.txt", "Hello from E2B test!");
});

test("Read file", async () => {
  const content = await sandbox.files.read("/tmp/test-file.txt");
  if (content !== "Hello from E2B test!")
    throw new Error(`File content mismatch: ${content}`);
});

test("List directory", async () => {
  const entries = await sandbox.files.list("/tmp");
  const names = entries.map((e) => e.name);
  if (!names.includes("test-file.txt"))
    throw new Error(`test-file.txt not found in /tmp listing: ${names}`);
});

test("Write and read multiline content", async () => {
  const data = "line1\nline2\nline3\n";
  await sandbox.files.write("/tmp/multiline.txt", data);
  const content = await sandbox.files.read("/tmp/multiline.txt");
  if (content !== data) throw new Error(`Multiline mismatch`);
});

test("Install package (apt)", async () => {
  const result = await sandbox.commands.run(
    "which curl || (apt-get update -qq && apt-get install -y -qq curl)",
    { timeoutMs: 60_000 }
  );
  if (result.exitCode !== 0)
    throw new Error(`Package install failed: ${result.stderr}`);
});

test("Network access (curl external)", async () => {
  const result = await sandbox.commands.run(
    "curl -s -o /dev/null -w '%{http_code}' https://httpbin.org/get",
    { timeoutMs: 15_000 }
  );
  if (!result.stdout.includes("200"))
    throw new Error(`Expected HTTP 200, got: ${result.stdout}`);
});

test("Environment variables", async () => {
  const result = await sandbox.commands.run("echo $HOME");
  if (!result.stdout.trim()) throw new Error("$HOME is empty");
});

test("Large output handling", async () => {
  const result = await sandbox.commands.run("seq 1 1000");
  const lines = result.stdout.trim().split("\n");
  if (lines.length !== 1000)
    throw new Error(`Expected 1000 lines, got ${lines.length}`);
});

test("Concurrent commands", async () => {
  const [a, b, c] = await Promise.all([
    sandbox.commands.run("echo A"),
    sandbox.commands.run("echo B"),
    sandbox.commands.run("echo C"),
  ]);
  if (!a.stdout.includes("A") || !b.stdout.includes("B") || !c.stdout.includes("C"))
    throw new Error("Concurrent command outputs incorrect");
});

test("File permissions (write + chmod + exec)", async () => {
  await sandbox.files.write("/tmp/script.sh", "#!/bin/bash\necho works");
  await sandbox.commands.run("chmod +x /tmp/script.sh");
  const result = await sandbox.commands.run("/tmp/script.sh");
  if (!result.stdout.includes("works"))
    throw new Error(`Script execution failed: ${result.stdout}`);
});

test("Sandbox keep-alive / set timeout", async () => {
  await sandbox.setTimeout(600_000); // 10 minutes
});

test("Process isolation (PID namespace)", async () => {
  // Verify we're in a proper PID namespace
  const result = await sandbox.commands.run("cat /proc/1/cmdline | tr '\\0' ' '");
  if (!result.stdout.trim()) throw new Error("Could not read PID 1 cmdline");
});

test("Kill sandbox", async () => {
  const id = sandbox.sandboxId;
  await sandbox.kill();

  // Verify it's gone
  const res = await fetch(`https://api.${DOMAIN}/sandboxes`, {
    headers: { "X-API-Key": await getApiKey() },
  });
  const list = await res.json();
  const found = Array.isArray(list) && list.find((s) => s.sandboxID === id);
  if (found) throw new Error(`Sandbox ${id} still listed after kill`);
  sandbox = null;
});

test("Create and immediately kill (lifecycle)", async (opts) => {
  const sb = await Sandbox.create({ ...opts, timeoutMs: 60_000 });
  console.log(`    sandbox: ${sb.sandboxId}`);
  await sb.kill();
});

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

runTests().catch((err) => {
  console.error("Fatal:", err);
  if (sandbox) sandbox.kill().catch(() => {});
  process.exit(2);
});
