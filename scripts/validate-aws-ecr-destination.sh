#!/usr/bin/env bash

set -euo pipefail

destination="${AWS_WRITE_DESTINATION:-}"
expected_prefix="${AWS_WRITE_EXPECTED_ECR_PREFIX:-}"
target="${SIG_AWS_ACCOUNT_TARGET:-unset}"

fail() {
  printf 'ECR destination authority failed: %s\n' "$*" >&2
  exit 1
}

[[ -n "${destination}" ]] ||
  fail "destination is required for target ${target}"
[[ -n "${expected_prefix}" ]] ||
  fail "expected registry prefix is unavailable for target ${target}"

# These Make targets append their own image tags. The authority value must be
# one untagged private-ECR repository, not a list of Docker CLI arguments.
[[ "${destination}" == "${expected_prefix}"* ]] ||
  fail "destination does not match target ${target} (${expected_prefix}...)"

repository="${destination#"${expected_prefix}"}"

# AWS ECR repository names are 2-256 characters. Each slash-delimited segment
# begins and ends with a lowercase alphanumeric character; dot, underscore,
# and hyphen are permitted only as separators. This deliberately excludes
# tags, digests, whitespace, shell metacharacters, flags, and empty segments.
if ((${#repository} < 2 || ${#repository} > 256)); then
  fail "repository path must contain 2-256 characters"
fi

repository_pattern='^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$'
[[ "${repository}" =~ ${repository_pattern} ]] ||
  fail "destination is not a valid untagged ECR repository for target ${target}"
