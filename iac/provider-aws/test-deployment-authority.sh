#!/usr/bin/env bash

set -euo pipefail

provider_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "${fixture_dir}"' EXIT

mkdir -p "${fixture_dir}/bin"

cat >"${fixture_dir}/bin/aws" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"sts get-caller-identity"* ]]; then
  printf '%s\n' '014155356804'
  exit 0
fi
printf 'unexpected fake aws invocation: %s\n' "$*" >&2
exit 1
EOF
chmod +x "${fixture_dir}/bin/aws"

common_args=(
  --no-print-directory
  -C "${provider_dir}"
  ENV=dev
  SIG_AWS_ACCOUNT_TARGET=cq
  AWS_PROFILE=test-cq
  AWS_ACCOUNT_ID=014155356804
  AWS_REGION=us-east-1
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate
  TF=terraform
)

export PATH="${fixture_dir}/bin:${PATH}"

fail() {
  printf 'deployment authority test failed: %s\n' "$*" >&2
  exit 1
}

if make --no-print-directory -C "${provider_dir}" aws-account-guard \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=foo \
  AWS_PROFILE=attacker \
  AWS_ACCOUNT_ID=000000000000 \
  AWS_ACCOUNT_ID_foo=000000000000 \
  AWS_REGION=us-east-1 \
  AWS_REGION_foo=us-east-1 \
  TERRAFORM_STATE_BUCKET=attacker-state \
  TF_STATE_BUCKET_foo=attacker-state \
  TF_VAR_FILE=./dev.cq.tfvars \
  TF=terraform \
  >"${fixture_dir}/unsupported-target.log" 2>&1; then
  fail 'caller-defined account target namespace was accepted'
fi
grep -q 'Unsupported SIG_AWS_ACCOUNT_TARGET: foo (expected cq or sig)' \
  "${fixture_dir}/unsupported-target.log" ||
  fail 'unsupported account target rejection was not explicit'

if make "${common_args[@]}" aws-account-guard \
  ENV=staging \
  CANONICAL_CQ_ENV=staging \
  CANONICAL_CQ_TF_VAR_FILE=./staging.cq.tfvars \
  TF_VAR_FILE=./staging.cq.tfvars \
  >"${fixture_dir}/cq-environment.log" 2>&1; then
  fail 'caller-selected CQ environment and unreviewed tfvars were accepted'
fi
grep -q 'CQ target is bound to the reviewed dev environment; got ENV=staging' \
  "${fixture_dir}/cq-environment.log" ||
  fail 'CQ environment authority rejection was not explicit'

if make "${common_args[@]}" aws-account-guard TF_VAR_FILE=/tmp/unreviewed.tfvars >"${fixture_dir}/var-file.log" 2>&1; then
  fail 'caller-controlled TF_VAR_FILE was accepted for CQ'
fi
grep -q 'CQ does not accept TF_VAR_FILE overrides' "${fixture_dir}/var-file.log" ||
  fail 'TF_VAR_FILE rejection was not explicit'

if make "${common_args[@]}" aws-account-guard TF_PLAN_FILE=.tfplan.dev.sig >"${fixture_dir}/plan-file.log" 2>&1; then
  fail 'caller-controlled TF_PLAN_FILE was accepted'
fi
grep -q 'TF_PLAN_FILE is derived from account authority' "${fixture_dir}/plan-file.log" ||
  fail 'TF_PLAN_FILE rejection was not explicit'

if make "${common_args[@]}" aws-account-guard \
  AWS_ACCOUNT_ID=000000000000 AWS_ACCOUNT_ID_cq=000000000000 EXPECTED_AWS_ACCOUNT_ID=000000000000 \
  >"${fixture_dir}/constant-account.log" 2>&1; then
  fail 'caller redefined the trusted CQ account constant'
fi
grep -q 'does not match target cq (014155356804)' "${fixture_dir}/constant-account.log" ||
  fail 'CQ account authority constants were not immutable'

if make "${common_args[@]}" aws-account-guard \
  TF_PLAN_FILE=.tfplan.attacker EXPECTED_TF_PLAN_FILE=.tfplan.attacker \
  >"${fixture_dir}/constant-plan.log" 2>&1; then
  fail 'caller redefined the trusted plan path'
fi
grep -q 'use .tfplan.dev.cq' "${fixture_dir}/constant-plan.log" ||
  fail 'target-bound plan authority was not immutable'

dry_run_plan="$(make "${common_args[@]}" -n plan PREFIX=unreviewed-)"
[[ "${dry_run_plan}" == *"-var-file=./dev.cq.tfvars"* ]] ||
  fail 'CQ did not select the canonical dev.cq.tfvars'
[[ "${dry_run_plan}" != *"TF_VAR_prefix=unreviewed-"* ]] ||
  fail 'environment-derived TF_VAR_prefix overrode reviewed CQ tfvars'

dry_run_apply="$(make "${common_args[@]}" -n apply)"
[[ "${dry_run_apply}" == *".tfplan.dev.cq"* ]] ||
  fail 'apply did not use the CQ-bound plan path'

dry_run_init="$(make "${common_args[@]}" -n init)"
[[ "${dry_run_init}" == *"terraform apply -var-file=./dev.cq.tfvars -target=module.init"* ]] ||
  fail 'init apply did not use the canonical CQ variables'

if make "${common_args[@]}" aws-account-guard TF_VAR_prefix=unreviewed- >"${fixture_dir}/override.log" 2>&1; then
  fail 'ambient TF_VAR_prefix was accepted for CQ'
fi
grep -q 'CQ rejects ambient Terraform variable overrides: TF_VAR_prefix' "${fixture_dir}/override.log" ||
  fail 'ambient TF_VAR rejection was not explicit'

for cli_var in TF_CLI_ARGS TF_CLI_ARGS_plan; do
  if make "${common_args[@]}" aws-account-guard "${cli_var}=-destroy" >"${fixture_dir}/cli-args.log" 2>&1; then
    fail "ambient ${cli_var} was accepted for CQ"
  fi
  grep -q "CQ rejects ambient Terraform CLI arguments: ${cli_var}" "${fixture_dir}/cli-args.log" ||
    fail "ambient ${cli_var} rejection was not explicit"
done

grep -q '^bucket_prefix = "e2b-014155356804-"[[:space:]]*$' "${provider_dir}/dev.cq.tfvars" ||
  fail 'canonical CQ tfvars omitted the required bucket_prefix'

make "${common_args[@]}" aws-account-guard

sig_args=(
  --no-print-directory
  -C "${provider_dir}"
  ENV=dev
  SIG_AWS_ACCOUNT_TARGET=sig
  AWS_PROFILE=test-sig
  AWS_ACCOUNT_ID=319933937176
  AWS_REGION=us-east-1
  TERRAFORM_STATE_BUCKET=superintelligent-group-terraform-state
  TF=terraform
  TF_VAR_FILE=./dev.sig.tfvars.reference
)

if make "${sig_args[@]}" aws-account-guard AWS_REGION=us-west-2 >"${fixture_dir}/sig-region.log" 2>&1; then
  fail 'noncanonical SIG rollback region was accepted'
fi
grep -q 'does not match target sig (us-east-1)' "${fixture_dir}/sig-region.log" ||
  fail 'SIG rollback region rejection was not explicit'

if make "${sig_args[@]}" aws-account-guard TERRAFORM_STATE_BUCKET=unreviewed-state >"${fixture_dir}/sig-state.log" 2>&1; then
  fail 'noncanonical SIG rollback state bucket was accepted'
fi
grep -q 'does not match target sig (superintelligent-group-terraform-state)' "${fixture_dir}/sig-state.log" ||
  fail 'SIG rollback state-bucket rejection was not explicit'

if grep -Fq '$(file <' "${provider_dir}/Makefile"; then
  fail 'Makefile still requires GNU Make 4 file-read syntax'
fi

printf 'AWS deployment authority checks passed.\n'
