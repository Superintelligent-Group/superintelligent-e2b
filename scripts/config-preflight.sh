#!/usr/bin/env bash
# =============================================================================
# config-preflight.sh — check the deploy config is complete and safe before use.
#
# Two invariants, checked together because they are two halves of one rule:
# deployment config is tracked in git, credentials live in AWS Secrets Manager.
#
#   1. NO SECRETS IN TRACKED CONFIG. .env.dev and *.tfvars are tracked (see the
#      note in .gitignore). That is only safe if it stays true that they hold no
#      credentials. Asserting it in a comment is not enforcement; this is.
#
#   2. EVERY REQUIRED SECRET EXISTS AND IS POPULATED. A secret that exists but
#      holds "" or "-" is worse than one that is absent: Terraform and the wake
#      Lambda both read it happily and fail later, somewhere unrelated. Audited
#      2026-07-27 and four were in exactly that state — including
#      e2b-dev/e2b-api-key, whose emptiness surfaced days later as a
#      MissingApiKey error in an entirely different subsystem (SUP-646).
#
# Usage:
#   AWS_PROFILE=commonquant-boot ./scripts/config-preflight.sh
#   ./scripts/config-preflight.sh --expect-account 014155356804
#
# Exit 0 = safe to apply. Exit 1 = a hard failure. Exit 2 = warnings only.
# =============================================================================
set -uo pipefail
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"
export MSYS_NO_PATHCONV=1

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPECT_ACCOUNT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --expect-account) EXPECT_ACCOUNT="$2"; shift 2 ;;
    *) shift ;;
  esac
done

fail=0
warn=0
say() { printf '%s\n' "$*"; }
bad() { printf '  FAIL  %s\n' "$*"; fail=$((fail + 1)); }
meh() { printf '  WARN  %s\n' "$*"; warn=$((warn + 1)); }
good() { printf '  ok    %s\n' "$*"; }

# --- account identity --------------------------------------------------------
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text 2>/dev/null || echo "")
if [ -z "$ACCOUNT_ID" ]; then
  say "FATAL: no usable AWS credentials (AWS_PROFILE=${AWS_PROFILE:-unset})."
  exit 1
fi
say ""
say "account ${ACCOUNT_ID}  profile ${AWS_PROFILE:-default}  region ${AWS_DEFAULT_REGION}"
if [ -n "$EXPECT_ACCOUNT" ] && [ "$EXPECT_ACCOUNT" != "$ACCOUNT_ID" ]; then
  say ""
  say "FATAL: expected account ${EXPECT_ACCOUNT}, credentials resolve to ${ACCOUNT_ID}."
  exit 1
fi

# --- 1. no credentials in tracked config -------------------------------------
say ""
say "tracked config carries no credentials"

TRACKED_CONFIG=".env.dev iac/provider-aws/dev.cq.tfvars iac/provider-aws/dev.sig.tfvars.reference"

# A credential is a long opaque value assigned to a credential-ish key. Bucket
# names, ARNs, AMI ids and instance types all contain digits and dashes, so
# match on the KEY plus a value long enough to be a real secret, and exempt the
# *_secret_id / *_SECRET_ID form, which is a reference BY id — exactly the
# pattern we want people using.
for f in $TRACKED_CONFIG; do
  [ -f "$ROOT/$f" ] || { meh "$f missing on disk"; continue; }
  hits=$(grep -nEi '^[[:space:]]*[A-Za-z_]*(secret|password|token|api_key|apikey|private_key|credential)[A-Za-z_]*[[:space:]]*=[[:space:]]*"?[A-Za-z0-9/+_-]{16,}' "$ROOT/$f" \
    | grep -vEi '(secret_id|secret_arn|_secret_ids)[[:space:]]*=' || true)
  if [ -n "$hits" ]; then
    bad "$f appears to inline a credential:"
    printf '%s\n' "$hits" | sed 's/=.*/=<redacted>/' | sed 's/^/          /'
  else
    good "$f"
  fi
done

# --- 2. required secrets exist and are populated ------------------------------
# Referenced by iac/provider-aws/auto-scaling.tf (wake Lambda) and the Nomad job
# specs. Keep these lists in sync when a new secret id enters the IaC.
#
# REQUIRED: the deploy cannot work without a real value.
# OPTIONAL: cluster_scaler.py guards with `if consul_token:` and skips cleanly
#   when the value is absent. Note it calls .strip() first, so a whitespace-only
#   secret is functionally absent for that consumer — which is exactly what
#   e2b-dev/consul-acl-token holds today (a single space). That is safe here and
#   still worth reporting, because it is ambiguous everywhere else: it renders as
#   "set" in the console, and any consumer that does not strip would forward a
#   space as a credential. Whitespace-only is a WARNING; a short non-whitespace
#   stub is a FAILURE, since nothing strips it away.
#
# Measure the STRIPPED length throughout, matching what the consumer actually
# sees. Measuring the raw length reports a whitespace placeholder as a populated
# 1-character secret, which is how this one went unnoticed.
REQUIRED_SECRETS="
e2b-dev/nomad-acl-token
e2b-dev/postgres-connection-string
e2b-dev/redis-url
e2b-dev/sandbox-access-token-hash-seed
e2b-dev/edge-api-secret
e2b-dev/e2b-api-key
e2b-dev/api-admin-token
"
OPTIONAL_SECRETS="
e2b-dev/consul-acl-token
"

# Read one at a time, not in a tight loop: Secrets Manager throttles, and a
# throttled GetSecretValue with stderr discarded is indistinguishable from an
# empty secret. That misreading produced a false "4 secrets are empty" audit on
# 2026-07-27 — do not reintroduce it by "optimising" this into a batch.
probe_secret() {
  local id="$1" out stripped
  out=$(aws secretsmanager get-secret-value --secret-id "$id" --query SecretString --output text 2>&1)
  if printf '%s' "$out" | grep -q "error occurred"; then
    printf 'ERR:%s' "$(printf '%s' "$out" | head -1 | cut -c1-60)"
    return
  fi
  stripped=$(printf '%s' "$out" | tr -d '[:space:]')
  if [ -n "$out" ] && [ -z "$stripped" ]; then
    printf 'BLANK:%s' "${#out}"   # present in the console, empty to any consumer that strips
  else
    printf 'LEN:%s' "${#stripped}"
  fi
}

say ""
say "required secrets are present and populated"
for s in $REQUIRED_SECRETS; do
  r=$(probe_secret "$s")
  case "$r" in
    ERR:*) bad "$s — could not be read: ${r#ERR:}" ;;
    LEN:0) bad "$s — does not exist or holds no value" ;;
    BLANK:*) bad "$s — holds only whitespace; every consumer that strips sees an empty required secret" ;;
    LEN:[1-7]) bad "$s — holds a ${r#LEN:}-character stub, not a real value" ;;
    *) good "$s (${r#LEN:} chars)" ;;
  esac
done

say ""
say "optional secrets are either absent or real — never a stub"
for s in $OPTIONAL_SECRETS; do
  r=$(probe_secret "$s")
  case "$r" in
    ERR:*) bad "$s — could not be read: ${r#ERR:}" ;;
    LEN:0) good "$s (absent — callers skip it cleanly)" ;;
    BLANK:*) meh "$s — ${r#BLANK:} whitespace char(s). cluster_scaler.py strips before testing, so it is skipped correctly; but it renders as \"set\" in the console and any consumer that does not strip would forward it. Clear it or set a real token." ;;
    LEN:[1-7]) bad "$s — ${r#LEN:}-character stub. Nothing strips this away, so callers testing truthiness WILL forward it as a credential." ;;
    *) good "$s (${r#LEN:} chars)" ;;
  esac
done

# --- 3. the config names an account that matches the credentials --------------
say ""
say "config and credentials agree on the account"
TFVARS="$ROOT/iac/provider-aws/dev.cq.tfvars"
if [ -f "$TFVARS" ]; then
  # Bucket names and ARNs embed the account id; a mismatch means the config
  # describes a different account than the one we are about to apply into.
  named=$(grep -oE '[0-9]{12}' "$TFVARS" | sort -u)
  for n in $named; do
    if [ "$n" = "$ACCOUNT_ID" ]; then
      good "dev.cq.tfvars references $n"
    else
      bad "dev.cq.tfvars references account $n but credentials are $ACCOUNT_ID"
    fi
  done
  [ -z "$named" ] && meh "dev.cq.tfvars names no account id — cannot cross-check"
else
  bad "iac/provider-aws/dev.cq.tfvars missing"
fi

say ""
if [ "$fail" -gt 0 ]; then
  say "BLOCKED — ${fail} failure(s), ${warn} warning(s). Do not apply."
  exit 1
fi
if [ "$warn" -gt 0 ]; then
  say "PASSED with ${warn} warning(s)."
  exit 2
fi
say "PASSED — config complete, no credentials tracked, account consistent."
exit 0
