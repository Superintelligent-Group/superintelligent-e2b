# =============================================================================
# Data Layer: RDS PostgreSQL, ElastiCache Redis
# =============================================================================

# -----------------------------------------------------------------------------
# RDS PostgreSQL
# -----------------------------------------------------------------------------

resource "aws_db_subnet_group" "main" {
  count = var.create_rds ? 1 : 0

  name       = "${var.prefix}-${var.environment}-db-subnet"
  subnet_ids = module.vpc.private_subnets

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-db-subnet-group"
  })
}

resource "aws_security_group" "rds" {
  count = var.create_rds ? 1 : 0

  name        = "${var.prefix}-${var.environment}-rds"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "PostgreSQL from control plane"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.control_plane.id]
  }

  ingress {
    description     = "PostgreSQL from workers"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.workers.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-rds-sg"
  })
}

resource "random_password" "rds_password" {
  count = var.create_rds ? 1 : 0

  length  = 32
  special = false
}

resource "aws_db_instance" "postgres" {
  count = var.create_rds ? 1 : 0

  identifier     = "${var.prefix}-${var.environment}-postgres"
  engine         = "postgres"
  engine_version = var.rds_postgres_version
  instance_class = var.rds_instance_class

  allocated_storage     = var.rds_allocated_storage
  max_allocated_storage = var.rds_max_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true
  kms_key_id            = var.rds_kms_key_id != "" ? var.rds_kms_key_id : null

  db_name  = var.rds_database_name
  username = var.rds_username
  password = random_password.rds_password[0].result

  db_subnet_group_name   = aws_db_subnet_group.main[0].name
  vpc_security_group_ids = [aws_security_group.rds[0].id]

  multi_az               = var.rds_multi_az
  publicly_accessible    = false
  deletion_protection    = var.environment == "prod"
  skip_final_snapshot    = var.environment != "prod"
  final_snapshot_identifier = var.environment == "prod" ? "${var.prefix}-${var.environment}-postgres-final" : null

  backup_retention_period = var.rds_backup_retention_days
  backup_window           = "03:00-04:00"
  maintenance_window      = "Mon:04:00-Mon:05:00"

  performance_insights_enabled          = var.rds_performance_insights
  performance_insights_retention_period = var.rds_performance_insights ? 7 : null

  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  parameter_group_name = aws_db_parameter_group.postgres[0].name

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-postgres"
  })
}

resource "aws_db_parameter_group" "postgres" {
  count = var.create_rds ? 1 : 0

  name   = "${var.prefix}-${var.environment}-postgres-params"
  family = "postgres${split(".", var.rds_postgres_version)[0]}"

  parameter {
    name  = "log_statement"
    value = "ddl"
  }

  parameter {
    name  = "log_min_duration_statement"
    value = "1000"
  }

  tags = var.tags
}

# -----------------------------------------------------------------------------
# ElastiCache Redis
# -----------------------------------------------------------------------------

resource "aws_elasticache_subnet_group" "main" {
  count = var.create_elasticache ? 1 : 0

  name       = "${var.prefix}-${var.environment}-redis-subnet"
  subnet_ids = module.vpc.private_subnets

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-redis-subnet-group"
  })
}

resource "aws_security_group" "redis" {
  count = var.create_elasticache ? 1 : 0

  name        = "${var.prefix}-${var.environment}-redis"
  description = "Security group for ElastiCache Redis"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Redis from control plane"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.control_plane.id]
  }

  ingress {
    description     = "Redis from workers"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.workers.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-redis-sg"
  })
}

resource "random_password" "redis_auth_token" {
  count = var.create_elasticache && var.redis_auth_enabled ? 1 : 0

  length  = 32
  special = false
}

resource "aws_elasticache_replication_group" "redis" {
  count = var.create_elasticache ? 1 : 0

  replication_group_id = "${var.prefix}-${var.environment}-redis"
  description          = "E2B Redis cluster for ${var.environment}"

  engine               = "redis"
  engine_version       = var.redis_engine_version
  node_type            = var.redis_node_type
  num_cache_clusters   = var.redis_num_cache_clusters
  port                 = 6379
  parameter_group_name = aws_elasticache_parameter_group.redis[0].name

  subnet_group_name  = aws_elasticache_subnet_group.main[0].name
  security_group_ids = [aws_security_group.redis[0].id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = var.redis_transit_encryption
  auth_token                 = var.redis_auth_enabled ? random_password.redis_auth_token[0].result : null

  automatic_failover_enabled = var.redis_num_cache_clusters > 1
  multi_az_enabled           = var.redis_num_cache_clusters > 1

  snapshot_retention_limit = var.redis_snapshot_retention_days
  snapshot_window          = "02:00-03:00"
  maintenance_window       = "sun:03:00-sun:04:00"

  apply_immediately = var.environment != "prod"

  tags = merge(var.tags, {
    Name = "${var.prefix}-${var.environment}-redis"
  })
}

resource "aws_elasticache_parameter_group" "redis" {
  count = var.create_elasticache ? 1 : 0

  name   = "${var.prefix}-${var.environment}-redis-params"
  family = "redis${split(".", var.redis_engine_version)[0]}"

  parameter {
    name  = "maxmemory-policy"
    value = "volatile-lru"
  }

  tags = var.tags
}

# -----------------------------------------------------------------------------
# Data Layer Outputs
# -----------------------------------------------------------------------------

output "rds_endpoint" {
  value       = var.create_rds ? aws_db_instance.postgres[0].endpoint : null
  description = "RDS PostgreSQL endpoint"
  sensitive   = false
}

output "rds_connection_string" {
  value       = var.create_rds ? "postgresql://${var.rds_username}:${random_password.rds_password[0].result}@${aws_db_instance.postgres[0].endpoint}/${var.rds_database_name}" : null
  description = "PostgreSQL connection string"
  sensitive   = true
}

output "redis_endpoint" {
  value       = var.create_elasticache ? aws_elasticache_replication_group.redis[0].primary_endpoint_address : null
  description = "ElastiCache Redis primary endpoint"
  sensitive   = false
}

output "redis_url" {
  value       = var.create_elasticache ? (var.redis_auth_enabled ? "rediss://:${random_password.redis_auth_token[0].result}@${aws_elasticache_replication_group.redis[0].primary_endpoint_address}:6379" : "redis://${aws_elasticache_replication_group.redis[0].primary_endpoint_address}:6379") : null
  description = "Redis connection URL"
  sensitive   = true
}
