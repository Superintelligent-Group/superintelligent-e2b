# =============================================================================
# AWS Nomad Jobs Deployment
# =============================================================================

terraform {
  required_providers {
    nomad = {
      source  = "hashicorp/nomad"
      version = "~> 2.0"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# -----------------------------------------------------------------------------
# Data Sources
# -----------------------------------------------------------------------------

data "aws_secretsmanager_secret_version" "postgres_connection_string" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/postgres-connection-string"
}

data "aws_secretsmanager_secret_version" "redis_url" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/redis-url"
}

data "aws_secretsmanager_secret_version" "supabase_jwt_secrets" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/supabase-jwt-secrets"
}

data "aws_secretsmanager_secret_version" "api_admin_token" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/api-admin-token"
}

data "aws_secretsmanager_secret_version" "nomad_acl_token" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/nomad-acl-token"
}

data "aws_secretsmanager_secret_version" "consul_acl_token" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/consul-acl-token"
}

data "aws_secretsmanager_secret_version" "posthog_api_key" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/posthog-api-key"
}

data "aws_secretsmanager_secret_version" "launch_darkly_api_key" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/launch-darkly-api-key"
}

data "aws_secretsmanager_secret_version" "analytics_collector" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/analytics-collector"
}

data "aws_secretsmanager_secret_version" "clickhouse" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/clickhouse"
}

data "aws_secretsmanager_secret_version" "sandbox_access_token_hash_seed" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/sandbox-access-token-hash-seed"
}

data "aws_secretsmanager_secret_version" "edge_api_secret" {
  count     = var.use_secrets_manager ? 1 : 0
  secret_id = "${var.prefix}-${var.environment}/edge-api-secret"
}

# -----------------------------------------------------------------------------
# Local Values
# -----------------------------------------------------------------------------

locals {
  postgres_connection_string = var.use_secrets_manager ? jsondecode(data.aws_secretsmanager_secret_version.postgres_connection_string[0].secret_string).connection_string : var.postgres_connection_string
  redis_url                  = var.use_secrets_manager ? jsondecode(data.aws_secretsmanager_secret_version.redis_url[0].secret_string).url : var.redis_url
  supabase_jwt_secrets       = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.supabase_jwt_secrets[0].secret_string : var.supabase_jwt_secrets
  admin_token                = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.api_admin_token[0].secret_string : var.admin_token
  nomad_acl_token            = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.nomad_acl_token[0].secret_string : var.nomad_acl_token
  consul_acl_token           = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.consul_acl_token[0].secret_string : var.consul_acl_token
  posthog_api_key            = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.posthog_api_key[0].secret_string : var.posthog_api_key
  launch_darkly_api_key      = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.launch_darkly_api_key[0].secret_string : var.launch_darkly_api_key

  analytics_collector        = var.use_secrets_manager ? jsondecode(data.aws_secretsmanager_secret_version.analytics_collector[0].secret_string) : { host = var.analytics_collector_host, api_token = var.analytics_collector_api_token }
  clickhouse_config          = var.use_secrets_manager ? jsondecode(data.aws_secretsmanager_secret_version.clickhouse[0].secret_string) : { connection_string = var.clickhouse_connection_string }

  sandbox_access_token_hash_seed = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.sandbox_access_token_hash_seed[0].secret_string : var.sandbox_access_token_hash_seed
  edge_api_secret            = var.use_secrets_manager ? data.aws_secretsmanager_secret_version.edge_api_secret[0].secret_string : var.edge_api_secret
}

# -----------------------------------------------------------------------------
# Nomad Provider Configuration
# -----------------------------------------------------------------------------

provider "nomad" {
  address   = var.nomad_address
  secret_id = local.nomad_acl_token
}

# -----------------------------------------------------------------------------
# Redis Job (if not using ElastiCache)
# -----------------------------------------------------------------------------

resource "nomad_job" "redis" {
  count = var.use_managed_redis ? 0 : 1

  jobspec = templatefile("${path.module}/jobs/redis.hcl", {
    aws_region  = var.aws_region
    node_pool   = var.api_node_pool
    port_number = var.redis_port
    port_name   = "redis"
  })
}

# -----------------------------------------------------------------------------
# API Job
# -----------------------------------------------------------------------------

resource "nomad_job" "api" {
  jobspec = templatefile("${path.module}/jobs/api.hcl", {
    aws_region     = var.aws_region
    node_pool      = var.api_node_pool
    count          = var.api_count
    update_stanza  = var.api_count > 1

    memory_mb = var.api_memory_mb
    cpu_count = var.api_cpu_count

    port_number                    = var.api_port
    orchestrator_port              = var.orchestrator_port
    otel_collector_grpc_endpoint   = "localhost:${var.otel_collector_grpc_port}"
    logs_collector_address         = "http://localhost:${var.logs_proxy_port}"
    api_docker_image               = var.api_docker_image
    db_migrator_docker_image       = var.db_migrator_docker_image
    postgres_connection_string     = local.postgres_connection_string
    supabase_jwt_secrets           = local.supabase_jwt_secrets
    posthog_api_key                = local.posthog_api_key
    environment                    = var.environment
    analytics_collector_host       = local.analytics_collector.host
    analytics_collector_api_token  = local.analytics_collector.api_token
    otel_tracing_print             = var.otel_tracing_print
    nomad_acl_token                = local.nomad_acl_token
    admin_token                    = local.admin_token
    redis_url                      = var.use_managed_redis ? "" : "redis.service.consul:${var.redis_port}"
    redis_cluster_url              = var.use_managed_redis ? local.redis_url : ""
    redis_tls_ca_base64            = ""
    clickhouse_connection_string   = local.clickhouse_config.connection_string
    sandbox_access_token_hash_seed = local.sandbox_access_token_hash_seed
    launch_darkly_api_key          = local.launch_darkly_api_key
    template_bucket_name           = var.template_bucket_name

    local_cluster_endpoint = "edge-api.service.consul:${var.edge_api_port}"
    local_cluster_token    = local.edge_api_secret
  })
}

# -----------------------------------------------------------------------------
# Client Proxy (Edge) Job
# -----------------------------------------------------------------------------

resource "nomad_job" "client_proxy" {
  jobspec = templatefile("${path.module}/jobs/edge.hcl", {
    aws_region          = var.aws_region
    node_pool           = var.api_node_pool
    count               = var.client_proxy_count
    update_stanza       = var.client_proxy_count > 1
    update_max_parallel = var.client_proxy_update_max_parallel

    memory_mb = var.client_proxy_memory_mb
    cpu_count = var.client_proxy_cpu_count

    environment         = var.environment
    redis_url           = var.use_managed_redis ? "" : "redis.service.consul:${var.redis_port}"
    redis_cluster_url   = var.use_managed_redis ? local.redis_url : ""
    redis_tls_ca_base64 = ""

    loki_url = "http://loki.service.consul:${var.loki_service_port}"

    proxy_port_name   = "proxy"
    proxy_port        = var.edge_proxy_port
    api_port_name     = "api"
    api_port          = var.edge_api_port
    api_secret        = local.edge_api_secret
    orchestrator_port = var.orchestrator_port

    image_name  = var.client_proxy_docker_image

    nomad_endpoint = "http://localhost:4646"
    nomad_token    = local.nomad_acl_token

    otel_collector_grpc_endpoint = "localhost:${var.otel_collector_grpc_port}"
    logs_collector_address       = "http://localhost:${var.logs_proxy_port}"
    launch_darkly_api_key        = local.launch_darkly_api_key
  })
}

# -----------------------------------------------------------------------------
# Orchestrator Job
# -----------------------------------------------------------------------------

resource "random_id" "orchestrator_job" {
  keepers = {
    orchestrator_checksum = var.orchestrator_checksum
  }

  byte_length = 8
}

locals {
  latest_orchestrator_job_id = var.environment == "dev" ? "dev" : random_id.orchestrator_job.hex
}

resource "nomad_variable" "orchestrator_hash" {
  path = "nomad/jobs"
  items = {
    latest_orchestrator_job_id = local.latest_orchestrator_job_id
  }
}

resource "nomad_job" "orchestrator" {
  deregister_on_id_change = false

  jobspec = templatefile("${path.module}/jobs/orchestrator.hcl", {
    aws_region                   = var.aws_region
    node_pool                    = var.orchestrator_node_pool
    latest_orchestrator_job_id   = local.latest_orchestrator_job_id
    port                         = var.orchestrator_port
    proxy_port                   = var.orchestrator_proxy_port
    environment                  = var.environment
    consul_acl_token             = local.consul_acl_token

    envd_timeout                 = var.envd_timeout
    bucket_name                  = var.build_bucket_name
    orchestrator_checksum        = var.orchestrator_checksum
    logs_collector_address       = "http://localhost:${var.logs_proxy_port}"
    otel_tracing_print           = var.otel_tracing_print
    template_bucket_name         = var.template_bucket_name
    otel_collector_grpc_endpoint = "localhost:${var.otel_collector_grpc_port}"
    allow_sandbox_internet       = var.allow_sandbox_internet
    launch_darkly_api_key        = local.launch_darkly_api_key
    clickhouse_connection_string = local.clickhouse_config.connection_string
    redis_url                    = var.use_managed_redis ? "" : "redis.service.consul:${var.redis_port}"
    redis_cluster_url            = var.use_managed_redis ? local.redis_url : ""
    redis_tls_ca_base64          = ""
    shared_chunk_cache_path      = var.shared_chunk_cache_path
  })

  depends_on = [nomad_variable.orchestrator_hash, random_id.orchestrator_job]
}

# -----------------------------------------------------------------------------
# Template Manager Job
# -----------------------------------------------------------------------------

resource "nomad_job" "template_manager" {
  jobspec = templatefile("${path.module}/jobs/template-manager.hcl", {
    aws_region    = var.aws_region
    node_pool     = var.builder_node_pool
    update_stanza = var.template_manager_count > 1
    port          = var.template_manager_port
    environment   = var.environment
    consul_acl_token = local.consul_acl_token

    api_secret                      = local.admin_token
    bucket_name                     = var.build_bucket_name
    docker_registry                 = var.ecr_repository_url
    template_manager_checksum       = var.template_manager_checksum
    otel_tracing_print              = var.otel_tracing_print
    template_bucket_name            = var.template_bucket_name
    build_cache_bucket_name         = var.build_cache_bucket_name
    otel_collector_grpc_endpoint    = "localhost:${var.otel_collector_grpc_port}"
    logs_collector_address          = "http://localhost:${var.logs_proxy_port}"
    orchestrator_services           = "template-manager"
    clickhouse_connection_string    = local.clickhouse_config.connection_string
    dockerhub_remote_repository_url = var.dockerhub_remote_repository_url
    launch_darkly_api_key           = local.launch_darkly_api_key
    shared_chunk_cache_path         = ""
  })
}
