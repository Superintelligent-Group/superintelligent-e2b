# Shared fail-closed authority for every Make target that can mutate AWS.
#
# Include this file from root or nested Makefiles, then add
# `aws-write-account-guard` as a prerequisite of each provider-aware write
# target. GCP remains a no-op; AWS delegates to the canonical provider guard so
# Terraform, Packer, S3, and ECR all enforce the same account decision.
#
# Identity alone is not sufficient: a principal can have cross-account access.
# Callers that write to ECR bind AWS_WRITE_DESTINATION to the registry they
# actually push, while S3 callers bind AWS_WRITE_S3_PREFIX to the bucket prefix
# used by their recipe. These bindings are `override` assignments in the owning
# Makefiles, so command-line variables cannot substitute a different claim.
#
# Export destination values rather than interpolating them into shell source.
# The validator can therefore inspect hostile input without executing it.
override AWS_ACCOUNT_AUTHORITY_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
override AWS_WRITE_ACCOUNT_REQUIRED = $(strip $(if $(filter aws,$(PROVIDER)),true, \
	$(if $(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION)),true,false)))
override AWS_WRITE_EXPECTED_ACCOUNT_cq := 014155356804
override AWS_WRITE_EXPECTED_ACCOUNT_sig := 319933937176
override AWS_WRITE_EXPECTED_ACCOUNT_ID = $(AWS_WRITE_EXPECTED_ACCOUNT_$(SIG_AWS_ACCOUNT_TARGET))
override AWS_WRITE_EXPECTED_ECR_PREFIX = $(AWS_WRITE_EXPECTED_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/
override AWS_WRITE_EXPECTED_S3_PREFIX = e2b-$(AWS_WRITE_EXPECTED_ACCOUNT_ID)-

export AWS_WRITE_DESTINATION AWS_WRITE_EXPECTED_ECR_PREFIX SIG_AWS_ACCOUNT_TARGET

.PHONY: aws-write-account-guard
aws-write-account-guard:
	$(if $(and $(AWS_WRITE_EXPECTED_ACCOUNT_ID),$(or $(and $(filter aws,$(PROVIDER)),$(filter true,$(AWS_WRITE_ECR_DESTINATION_REQUIRED))), \
		$(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION)))), \
		$(if $(strip $(AWS_WRITE_DESTINATION)), \
			$(if $(filter 1,$(words $(AWS_WRITE_DESTINATION))), \
				$(if $(filter $(AWS_WRITE_EXPECTED_ECR_PREFIX)%,$(AWS_WRITE_DESTINATION)),, \
					$(error ECR destination $(AWS_WRITE_DESTINATION) does not match target $(SIG_AWS_ACCOUNT_TARGET) ($(AWS_WRITE_EXPECTED_ECR_PREFIX)...))), \
				$(error ECR destination must be exactly one registry reference for target $(SIG_AWS_ACCOUNT_TARGET))), \
			$(error ECR destination is required for target $(SIG_AWS_ACCOUNT_TARGET))))
	$(if $(and $(filter aws,$(PROVIDER)),$(AWS_WRITE_EXPECTED_ACCOUNT_ID),$(filter true,$(AWS_WRITE_S3_DESTINATION_REQUIRED))), \
		$(if $(strip $(AWS_WRITE_S3_PREFIX)), \
			$(if $(filter 1,$(words $(AWS_WRITE_S3_PREFIX))), \
				$(if $(filter $(AWS_WRITE_EXPECTED_S3_PREFIX),$(AWS_WRITE_S3_PREFIX)),, \
					$(error S3 bucket prefix $(AWS_WRITE_S3_PREFIX) does not match target $(SIG_AWS_ACCOUNT_TARGET) ($(AWS_WRITE_EXPECTED_S3_PREFIX)))), \
				$(error S3 bucket prefix must be exactly one value for target $(SIG_AWS_ACCOUNT_TARGET))), \
			$(error S3 bucket prefix is required for target $(SIG_AWS_ACCOUNT_TARGET))))
	+@if [ -n "$(AWS_WRITE_EXPECTED_ACCOUNT_ID)" ] && \
		{ [ "$(AWS_WRITE_ECR_DESTINATION_REQUIRED)" = "true" ] || [ -n "$(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION))" ]; }; then \
		bash "$(AWS_ACCOUNT_AUTHORITY_ROOT)/scripts/validate-aws-ecr-destination.sh"; \
	fi
	+@if [ "$(AWS_WRITE_ACCOUNT_REQUIRED)" = "true" ]; then \
		make --no-print-directory -C $(AWS_ACCOUNT_AUTHORITY_ROOT)/iac/provider-aws aws-account-guard; \
	fi
