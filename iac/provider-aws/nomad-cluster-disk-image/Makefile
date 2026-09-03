include ../../../scripts/make-raw-input-authority.mk
$(call aws_guard_writer_inputs)
ENV := $(shell cat ../../../.last_used_env || echo "not-set")
-include ../../../.env.${ENV}
$(call aws_guard_writer_inputs)
# The shared account guard runs as a recursive make in provider-aws. Export
# the resolved, already-validated environment so that child does not recreate
# ENV as an unevaluated `$(shell ...)` default and reject its own expression.
export ENV

include ../../../scripts/aws-account-authority.mk

init:
	packer init -upgrade .

build: aws-write-account-guard
	packer build \
		$(if $(PACKER_CONSUL_VERSION),-var "consul_version=$(PACKER_CONSUL_VERSION)") \
		$(if $(PACKER_NOMAD_VERSION),-var "nomad_version=$(PACKER_NOMAD_VERSION)") \
		$(if $(PACKER_FIRECRACKER_HOST_VERSION),-var "firecracker_host_version=$(PACKER_FIRECRACKER_HOST_VERSION)") \
		$(if $(PACKER_FIRECRACKER_RUNTIME_VERSION),-var "firecracker_runtime_version=$(PACKER_FIRECRACKER_RUNTIME_VERSION)") \
		$(if $(PACKER_FIRECRACKER_RUNTIME_SHA256),-var "firecracker_runtime_sha256=$(PACKER_FIRECRACKER_RUNTIME_SHA256)") \
		$(if $(PACKER_CLOUDWATCH_AGENT_VERSION),-var "cloudwatch_agent_version=$(PACKER_CLOUDWATCH_AGENT_VERSION)") \
		$(if $(PACKER_CLOUDWATCH_AGENT_SHA256),-var "cloudwatch_agent_sha256=$(PACKER_CLOUDWATCH_AGENT_SHA256)") \
		$(if $(PACKER_VPC_ID),-var "vpc_id=$(PACKER_VPC_ID)") \
		$(if $(PACKER_SUBNET_ID),-var "subnet_id=$(PACKER_SUBNET_ID)") \
		$(if $(PACKER_SOURCE_AMI_FILTER_NAME),-var "source_ami_filter_name=$(PACKER_SOURCE_AMI_FILTER_NAME)") \
		-var "prefix=$(PREFIX)" \
		-var "aws_region=$(AWS_REGION)" \
		-var "aws_profile=$(AWS_PROFILE)" \
		.

format:
	packer fmt .
