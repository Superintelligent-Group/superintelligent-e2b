include ../../scripts/make-raw-input-authority.mk
$(call aws_guard_writer_inputs)
ENV := $(shell cat ../../.last_used_env || echo "not-set")
-include ../../.env.${ENV}
$(call aws_guard_writer_inputs)

include ../../scripts/aws-account-authority.mk

AWS_BUCKET_PREFIX ?= $(PREFIX)$(AWS_ACCOUNT_ID)-
GCP_BUCKET_PREFIX ?= $(GCP_PROJECT_ID)-
override AWS_WRITE_S3_PREFIX = $(AWS_BUCKET_PREFIX)
override AWS_WRITE_S3_PREFIX_SOURCE := AWS_BUCKET_PREFIX
override AWS_WRITE_S3_DESTINATION_REQUIRED := true
unexport AWS_BUCKET_PREFIX

PLUGIN_NAME := nomad-nodepool-apm
TARGET_PLUGIN_NAME := nomad-deployment-aware-target
BIN_DIR := bin
BINARY := $(BIN_DIR)/$(PLUGIN_NAME)
TARGET_BINARY := $(BIN_DIR)/$(TARGET_PLUGIN_NAME)

.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY) .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(TARGET_BINARY) ./cmd/nomad-deployment-aware-target

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)

.PHONY: test
test:
	go test -v ./...

.PHONY: upload
upload: aws-write-account-guard
	$(eval COMMIT_SHA := $(shell git rev-parse --short HEAD))
	chmod +x $(BINARY)
	chmod +x $(TARGET_BINARY)
ifeq ($(PROVIDER),aws)
	aws s3 cp $(BINARY) "s3://${AWS_BUCKET_PREFIX}fc-env-pipeline/nomad-nodepool-apm" --profile ${AWS_PROFILE} --cache-control "no-cache, max-age=0"
	aws s3 cp $(BINARY) "s3://${AWS_BUCKET_PREFIX}fc-env-pipeline/nomad-nodepool-apm.$(COMMIT_SHA)" --profile ${AWS_PROFILE} --cache-control "no-cache, max-age=0"
	aws s3 cp $(TARGET_BINARY) "s3://${AWS_BUCKET_PREFIX}fc-env-pipeline/nomad-deployment-aware-target" --profile ${AWS_PROFILE} --cache-control "no-cache, max-age=0"
	aws s3 cp $(TARGET_BINARY) "s3://${AWS_BUCKET_PREFIX}fc-env-pipeline/nomad-deployment-aware-target.$(COMMIT_SHA)" --profile ${AWS_PROFILE} --cache-control "no-cache, max-age=0"
else
	gcloud storage cp --cache-control="no-cache, max-age=0" $(BINARY) "gs://${GCP_BUCKET_PREFIX}fc-env-pipeline/nomad-nodepool-apm"
	gcloud storage cp --cache-control="no-cache, max-age=0" $(BINARY) "gs://${GCP_BUCKET_PREFIX}fc-env-pipeline/nomad-nodepool-apm.$(COMMIT_SHA)"
	gcloud storage cp --cache-control="no-cache, max-age=0" $(TARGET_BINARY) "gs://${GCP_BUCKET_PREFIX}fc-env-pipeline/nomad-deployment-aware-target"
	gcloud storage cp --cache-control="no-cache, max-age=0" $(TARGET_BINARY) "gs://${GCP_BUCKET_PREFIX}fc-env-pipeline/nomad-deployment-aware-target.$(COMMIT_SHA)"
endif

.PHONY: build-and-upload
build-and-upload: build upload
