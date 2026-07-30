# Shared fail-closed authority for every Make target that can mutate AWS.
#
# Include this file from root or nested Makefiles, then add
# `aws-write-account-guard` as a prerequisite of each provider-aware write
# target. GCP remains a no-op; AWS delegates to the canonical provider guard so
# Terraform, Packer, S3, and ECR all enforce the same account decision.
override AWS_ACCOUNT_AUTHORITY_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
override AWS_WRITE_ACCOUNT_REQUIRED = $(strip $(if $(filter aws,$(PROVIDER)),true, \
	$(if $(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION)),true,false)))

.PHONY: aws-write-account-guard
aws-write-account-guard:
	+@if [ "$(AWS_WRITE_ACCOUNT_REQUIRED)" = "true" ]; then \
		make --no-print-directory -C $(AWS_ACCOUNT_AUTHORITY_ROOT)/iac/provider-aws aws-account-guard; \
	fi
