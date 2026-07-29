# SUP-676: e2b's own dedicated, self-contained Postgres. This is what the
# api job's own db-migrator prestart task and main task both connect to
# (see nomad/main.tf's api_env_vars/api_db_migrator_env_vars, both sourced
# from module.init.postgres_connection_string below). Deliberately NOT the
# product's shared vpc-dev RDS -- that would need cross-VPC connectivity
# for no real reason: a sandbox platform's own bookkeeping has no reason to
# share a database instance with the main app.
#
# Computed and written to the secret entirely within this module (no
# placeholder + manual post-deploy population step, unlike the previous,
# never-populated version of this secret) -- see the secret_string
# expression in secrets.tf.

resource "random_password" "postgres_master" {
  count = var.create_rds ? 1 : 0

  length  = 32
  special = false

  lifecycle {
    ignore_changes = [length, special]
  }
}

resource "aws_db_subnet_group" "postgres" {
  count = var.create_rds ? 1 : 0

  name       = "${var.prefix}postgres"
  subnet_ids = module.network.vpc_private_subnets
}

resource "aws_security_group" "postgres" {
  count = var.create_rds ? 1 : 0

  name        = "${var.prefix}postgres"
  description = "e2b dedicated Postgres, ingress from this VPC only (private subnet, no public route)"
  vpc_id      = module.network.vpc_id

  ingress {
    description = "Postgres from cluster nodes"
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = [module.network.vpc_cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.prefix}postgres"
  }
}

resource "aws_db_instance" "postgres" {
  count = var.create_rds ? 1 : 0

  identifier = "${var.prefix}postgres"

  engine         = "postgres"
  engine_version = "15"
  instance_class = var.rds_instance_class

  allocated_storage     = var.rds_allocated_storage
  max_allocated_storage = var.rds_allocated_storage * 4
  storage_type          = "gp3"
  storage_encrypted     = true

  # SUP-676: upstream's migrations reference a role named "postgres" (an
  # explicit GRANT/ALTER ... OWNER TO in one of the later migrations,
  # confirmed via a failed first attempt: `role "postgres" does not exist`)
  # -- match that rather than fight it. db_name stays "e2b"; it's just the
  # schema, unrelated to the master role name.
  db_name  = "e2b"
  username = "postgres"
  password = random_password.postgres_master[0].result
  port     = 5432

  db_subnet_group_name   = aws_db_subnet_group.postgres[0].name
  vpc_security_group_ids = [aws_security_group.postgres[0].id]

  multi_az                     = var.rds_multi_az
  performance_insights_enabled = var.rds_performance_insights
  publicly_accessible          = false
  auto_minor_version_upgrade   = true

  backup_retention_period = 1
  skip_final_snapshot     = true
  deletion_protection     = false

  tags = {
    Name = "${var.prefix}postgres"
  }
}
