# =============================================================================
# AWS Secrets Manager Configuration
# =============================================================================

# -----------------------------------------------------------------------------
# Database Secrets
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "postgres_connection_string" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/postgres-connection-string"
  description = "PostgreSQL connection string for E2B services"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "postgres_connection_string" {
  count = var.create_secrets && var.create_rds ? 1 : 0

  secret_id = aws_secretsmanager_secret.postgres_connection_string[0].id
  secret_string = jsonencode({
    connection_string = "postgresql://${var.rds_username}:${random_password.rds_password[0].result}@${aws_db_instance.postgres[0].endpoint}/${var.rds_database_name}"
    host              = aws_db_instance.postgres[0].address
    port              = 5432
    database          = var.rds_database_name
    username          = var.rds_username
    password          = random_password.rds_password[0].result
  })
}

resource "aws_secretsmanager_secret" "redis_url" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/redis-url"
  description = "Redis connection URL for E2B services"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "redis_url" {
  count = var.create_secrets && var.create_elasticache ? 1 : 0

  secret_id = aws_secretsmanager_secret.redis_url[0].id
  secret_string = jsonencode({
    url           = var.redis_auth_enabled ? "rediss://:${random_password.redis_auth_token[0].result}@${aws_elasticache_replication_group.redis[0].primary_endpoint_address}:6379" : "redis://${aws_elasticache_replication_group.redis[0].primary_endpoint_address}:6379"
    host          = aws_elasticache_replication_group.redis[0].primary_endpoint_address
    port          = 6379
    auth_token    = var.redis_auth_enabled ? random_password.redis_auth_token[0].result : ""
    tls_enabled   = var.redis_transit_encryption
  })
}

# -----------------------------------------------------------------------------
# Authentication Secrets
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "supabase_jwt_secrets" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/supabase-jwt-secrets"
  description = "Supabase JWT secrets for authentication"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "supabase_jwt_secrets" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.supabase_jwt_secrets[0].id
  secret_string = var.supabase_jwt_secrets != "" ? var.supabase_jwt_secrets : " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "aws_secretsmanager_secret" "api_admin_token" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/api-admin-token"
  description = "Admin token for API authentication"

  tags = var.tags
}

resource "random_password" "api_admin_token" {
  count = var.create_secrets ? 1 : 0

  length  = 64
  special = false
}

resource "aws_secretsmanager_secret_version" "api_admin_token" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.api_admin_token[0].id
  secret_string = random_password.api_admin_token[0].result
}

# -----------------------------------------------------------------------------
# Nomad & Consul Secrets
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "nomad_acl_token" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/nomad-acl-token"
  description = "Nomad ACL bootstrap token"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "nomad_acl_token" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.nomad_acl_token[0].id
  secret_string = var.nomad_acl_token != "" ? var.nomad_acl_token : " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "aws_secretsmanager_secret" "consul_acl_token" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/consul-acl-token"
  description = "Consul ACL bootstrap token"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "consul_acl_token" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.consul_acl_token[0].id
  secret_string = var.consul_acl_token != "" ? var.consul_acl_token : " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# -----------------------------------------------------------------------------
# Third-Party Service Secrets
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "posthog_api_key" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/posthog-api-key"
  description = "PostHog API key for analytics"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "posthog_api_key" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.posthog_api_key[0].id
  secret_string = var.posthog_api_key != "" ? var.posthog_api_key : " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "aws_secretsmanager_secret" "launch_darkly_api_key" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/launch-darkly-api-key"
  description = "LaunchDarkly API key for feature flags"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "launch_darkly_api_key" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.launch_darkly_api_key[0].id
  secret_string = var.launch_darkly_api_key != "" ? var.launch_darkly_api_key : " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "aws_secretsmanager_secret" "analytics_collector" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/analytics-collector"
  description = "Analytics collector configuration"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "analytics_collector" {
  count = var.create_secrets ? 1 : 0

  secret_id = aws_secretsmanager_secret.analytics_collector[0].id
  secret_string = jsonencode({
    host      = var.analytics_collector_host
    api_token = var.analytics_collector_api_token
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# -----------------------------------------------------------------------------
# ClickHouse Secrets
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "clickhouse" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/clickhouse"
  description = "ClickHouse connection details"

  tags = var.tags
}

resource "random_password" "clickhouse_password" {
  count = var.create_secrets && var.clickhouse_password == "" ? 1 : 0

  length  = 32
  special = false
}

resource "aws_secretsmanager_secret_version" "clickhouse" {
  count = var.create_secrets ? 1 : 0

  secret_id = aws_secretsmanager_secret.clickhouse[0].id
  secret_string = jsonencode({
    host              = var.clickhouse_host
    port              = var.clickhouse_port
    database          = var.clickhouse_database
    username          = var.clickhouse_username
    password          = var.clickhouse_password != "" ? var.clickhouse_password : (length(random_password.clickhouse_password) > 0 ? random_password.clickhouse_password[0].result : "")
    connection_string = var.clickhouse_host != "" ? "clickhouse://${var.clickhouse_username}:${var.clickhouse_password != "" ? var.clickhouse_password : (length(random_password.clickhouse_password) > 0 ? random_password.clickhouse_password[0].result : "")}@${var.clickhouse_host}:${var.clickhouse_port}/${var.clickhouse_database}" : ""
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# -----------------------------------------------------------------------------
# Sandbox Access Token Seed
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "sandbox_access_token_hash_seed" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/sandbox-access-token-hash-seed"
  description = "Hash seed for sandbox access tokens"

  tags = var.tags
}

resource "random_password" "sandbox_access_token_hash_seed" {
  count = var.create_secrets ? 1 : 0

  length  = 32
  special = false
}

resource "aws_secretsmanager_secret_version" "sandbox_access_token_hash_seed" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.sandbox_access_token_hash_seed[0].id
  secret_string = var.sandbox_access_token_hash_seed != "" ? var.sandbox_access_token_hash_seed : random_password.sandbox_access_token_hash_seed[0].result
}

# -----------------------------------------------------------------------------
# Edge/Client Proxy Secret
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "edge_api_secret" {
  count = var.create_secrets ? 1 : 0

  name        = "${var.prefix}-${var.environment}/edge-api-secret"
  description = "Secret for edge/client proxy API authentication"

  tags = var.tags
}

resource "random_password" "edge_api_secret" {
  count = var.create_secrets ? 1 : 0

  length  = 64
  special = false
}

resource "aws_secretsmanager_secret_version" "edge_api_secret" {
  count = var.create_secrets ? 1 : 0

  secret_id     = aws_secretsmanager_secret.edge_api_secret[0].id
  secret_string = random_password.edge_api_secret[0].result
}

# -----------------------------------------------------------------------------
# IAM Policy for Secrets Access
# -----------------------------------------------------------------------------

resource "aws_iam_role_policy" "control_plane_secrets_access" {
  count = var.create_secrets ? 1 : 0

  name = "${var.prefix}-${var.environment}-control-plane-secrets"
  role = aws_iam_role.control_plane.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [
          "arn:aws:secretsmanager:${var.aws_region}:*:secret:${var.prefix}-${var.environment}/*"
        ]
      }
    ]
  })
}

resource "aws_iam_role_policy" "worker_secrets_access" {
  count = var.create_secrets ? 1 : 0

  name = "${var.prefix}-${var.environment}-worker-secrets"
  role = aws_iam_role.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [
          "arn:aws:secretsmanager:${var.aws_region}:*:secret:${var.prefix}-${var.environment}/*"
        ]
      }
    ]
  })
}

# -----------------------------------------------------------------------------
# Secrets Outputs
# -----------------------------------------------------------------------------

output "secrets_arns" {
  value = var.create_secrets ? {
    postgres_connection_string    = aws_secretsmanager_secret.postgres_connection_string[0].arn
    redis_url                     = aws_secretsmanager_secret.redis_url[0].arn
    supabase_jwt_secrets          = aws_secretsmanager_secret.supabase_jwt_secrets[0].arn
    api_admin_token               = aws_secretsmanager_secret.api_admin_token[0].arn
    nomad_acl_token               = aws_secretsmanager_secret.nomad_acl_token[0].arn
    consul_acl_token              = aws_secretsmanager_secret.consul_acl_token[0].arn
    posthog_api_key               = aws_secretsmanager_secret.posthog_api_key[0].arn
    launch_darkly_api_key         = aws_secretsmanager_secret.launch_darkly_api_key[0].arn
    clickhouse                    = aws_secretsmanager_secret.clickhouse[0].arn
    sandbox_access_token_hash_seed = aws_secretsmanager_secret.sandbox_access_token_hash_seed[0].arn
    edge_api_secret               = aws_secretsmanager_secret.edge_api_secret[0].arn
  } : {}
  description = "ARNs of created secrets"
}
