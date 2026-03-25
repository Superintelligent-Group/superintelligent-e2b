#!/usr/bin/env python3
"""Seed E2B postgres with team, API key, tier, and base template.

Usage:
    python3 scripts/seed-db.py

Requires:
    - AWS CLI configured with access to the e2b-dev account
    - Nomad cluster running with postgres job healthy

This script uses Nomad JSON job specs (not HCL) to avoid quoting issues.
It submits SQL as batch jobs because there's no direct DB access.
"""
import json
import os
import subprocess
import urllib.request
import ssl
import hashlib
import base64
import time
import sys

ctx = ssl.create_default_context()

def aws_secret(name):
    return subprocess.check_output(
        ["aws", "secretsmanager", "get-secret-value",
         "--secret-id", name,
         "--query", "SecretString", "--output", "text",
         "--region", "us-east-1"],
        text=True, env={**os.environ, "MSYS_NO_PATHCONV": "1"},
    ).strip()

def nomad_request(method, path, body=None):
    url = f"{NOMAD_BASE}{path}"
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(url, data=data, headers=NOMAD_HEADERS, method=method)
    with urllib.request.urlopen(req, context=ctx) as resp:
        return json.loads(resp.read())

def find_postgres_ip():
    allocs = nomad_request("GET", "/v1/job/postgres/allocations")
    running = [a for a in allocs if a["ClientStatus"] == "running"]
    if not running:
        print("ERROR: No running postgres allocation!")
        sys.exit(1)
    node = nomad_request("GET", f"/v1/node/{running[0]['NodeID']}")
    return node["Attributes"]["unique.network.ip-address"]

def run_sql(job_name, pg_ip, sql):
    """Submit a batch job to run SQL against postgres. Returns True on success."""
    # Purge any existing job with same name
    try:
        nomad_request("DELETE", f"/v1/job/{job_name}?purge=true")
    except Exception:
        pass

    job_spec = {
        "Job": {
            "ID": job_name,
            "Name": job_name,
            "Type": "batch",
            "Datacenters": ["us-east-1c"],
            "NodePool": "api",
            "ConsulToken": CONSUL_TOKEN,
            "TaskGroups": [{
                "Name": "q",
                "Count": 1,
                "Tasks": [{
                    "Name": "q",
                    "Driver": "docker",
                    "Config": {
                        "image": "postgres:15-alpine",
                        "command": "psql",
                        "args": ["-h", pg_ip, "-p", "5432", "-U", "postgres", "-d", "e2b", "-c", sql]
                    },
                    "Env": {"PGPASSWORD": "e2b-postgres-pw"},
                    "Resources": {"CPU": 100, "MemoryMB": 128}
                }]
            }]
        }
    }

    result = nomad_request("POST", "/v1/jobs", job_spec)
    eval_id = result.get("EvalID", "?")
    print(f"  [{job_name}] submitted (eval {eval_id[:8]})")

    # Wait for completion
    for i in range(10):
        time.sleep(5)
        allocs = nomad_request("GET", f"/v1/job/{job_name}/allocations")
        if allocs:
            status = allocs[0]["ClientStatus"]
            if status == "complete":
                print(f"  [{job_name}] completed successfully")
                return True
            elif status == "failed":
                ts = allocs[0].get("TaskStates", {}).get("q", {})
                events = ts.get("Events", [])
                for ev in events[-3:]:
                    print(f"  [{job_name}] {ev.get('Type')}: {ev.get('DisplayMessage', '')} exit={ev.get('ExitCode', '')}")
                return False
    print(f"  [{job_name}] timed out")
    return False


# --- Main ---
print("Loading secrets...")
NOMAD_TOKEN = aws_secret("e2b-dev/nomad-acl-token")
CONSUL_TOKEN = aws_secret("e2b-dev/consul-acl-token")
E2B_KEY = aws_secret("e2b-dev/e2b-api-key")

NOMAD_BASE = "https://nomad.e2b.superintelligent.group"
NOMAD_HEADERS = {"X-Nomad-Token": NOMAD_TOKEN, "Content-Type": "application/json"}

pg_ip = find_postgres_ip()
print(f"Postgres: {pg_ip}")

# Compute API key hash (Go hex-decodes key value, then SHA-256 hashes raw bytes)
prefix = "e2b_"
key_value = E2B_KEY[len(prefix):]
key_bytes = bytes.fromhex(key_value)
h = hashlib.sha256(key_bytes).digest()
api_key_hash = "$sha256$" + base64.b64encode(h).decode().rstrip("=")
print(f"Key: {E2B_KEY[:12]}... hash: {api_key_hash}")

TEAM_ID = "a0000000-0000-0000-0000-000000000001"
BUILD_ID = "04dba22b-6819-4c42-8b61-57a636181ce9"

# Step 1: Tiers + Teams
print("\n1. Seeding tiers and teams...")
ok = run_sql("db-seed-1", pg_ip, (
    f"INSERT INTO tiers (id, name, concurrent_instances, max_length_hours, concurrent_template_builds) "
    f"VALUES ('base_v1', 'Base tier', 100, 24, 20) ON CONFLICT (id) DO NOTHING; "
    f"INSERT INTO teams (id, name, tier, email, created_at) VALUES "
    f"('{TEAM_ID}', 'Default Team', 'base_v1', 'dev@superintelligent.group', NOW()) "
    f"ON CONFLICT (id) DO NOTHING;"
))
if not ok:
    print("FAILED: tiers/teams seed")
    sys.exit(1)

# Step 2: API Key
print("\n2. Seeding API key...")
ok = run_sql("db-seed-2", pg_ip, (
    f"INSERT INTO team_api_keys (api_key_hash, team_id, api_key_prefix, api_key_length, "
    f"api_key_mask_prefix, api_key_mask_suffix, name, created_at) VALUES "
    f"('{api_key_hash}', '{TEAM_ID}', '{E2B_KEY[:8]}', {len(E2B_KEY)}, "
    f"'e2b_5', '{E2B_KEY[-4:]}', 'Default Key', NOW()) "
    f"ON CONFLICT (api_key_hash) DO NOTHING;"
))
if not ok:
    print("FAILED: API key seed")
    sys.exit(1)

# Step 3: Template (envs table - no dockerfile/build_id columns, those moved to env_builds)
print("\n3. Seeding template...")
ok = run_sql("db-seed-3", pg_ip, (
    f"INSERT INTO envs (id, created_at, updated_at, public, team_id) "
    f"VALUES ('base', NOW(), NOW(), true, '{TEAM_ID}') "
    f"ON CONFLICT (id) DO UPDATE SET updated_at = NOW();"
))
if not ok:
    print("FAILED: template env seed")
    sys.exit(1)

# Step 4: Template build
print("\n4. Seeding template build...")
ok = run_sql("db-seed-4", pg_ip, (
    f"INSERT INTO env_builds (id, created_at, updated_at, finished_at, status, dockerfile, "
    f"start_cmd, vcpu, ram_mb, free_disk_size_mb, total_disk_size_mb, kernel_version, "
    f"firecracker_version, envd_version, env_id) VALUES ('{BUILD_ID}', NOW(), NOW(), NOW(), 'uploaded', "
    f"'FROM e2bdev/base', '', 2, 512, 512, 1024, 'vmlinux-6.1.158', "
    f"'v1.12.1_a41d3fb', 'v0.1.1', 'base') ON CONFLICT (id) DO UPDATE SET envd_version = 'v0.1.1';"
))
if not ok:
    print("FAILED: template build seed")
    sys.exit(1)

# Step 5: Template alias
print("\n5. Seeding template alias...")
ok = run_sql("db-seed-5", pg_ip, (
    f"INSERT INTO env_aliases (alias, is_renamable, env_id) "
    f"VALUES ('base', true, 'base') ON CONFLICT DO NOTHING;"
))
if not ok:
    print("FAILED: template alias seed")
    sys.exit(1)

# Step 6: Build assignment (links env to build with tag)
print("\n6. Seeding build assignment...")
ok = run_sql("db-seed-6", pg_ip, (
    f"INSERT INTO env_build_assignments (env_id, build_id, tag) "
    f"VALUES ('base', '{BUILD_ID}', 'default') ON CONFLICT DO NOTHING;"
))
if not ok:
    print("FAILED: build assignment seed")
    sys.exit(1)

# Cleanup batch jobs
print("\nCleaning up batch jobs...")
for i in range(1, 7):
    try:
        nomad_request("DELETE", f"/v1/job/db-seed-{i}?purge=true")
    except Exception:
        pass

print("\nSeed complete! Testing API key...")
try:
    api_url = "https://api.e2b.superintelligent.group/sandboxes"
    req = urllib.request.Request(api_url, headers={"X-API-Key": E2B_KEY})
    with urllib.request.urlopen(req, context=ctx) as resp:
        data = json.loads(resp.read())
        print(f"API auth: OK (sandboxes: {len(data)})")
except urllib.error.HTTPError as e:
    print(f"API auth: FAILED ({e.code})")
