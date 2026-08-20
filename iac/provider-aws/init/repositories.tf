resource "aws_ecr_repository" "client_proxy" {
  name                 = "${var.prefix}core/client-proxy"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "clickhouse_migrator" {
  name                 = "${var.prefix}core/clickhouse-migrator"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "db_migrator" {
  name                 = "${var.prefix}core/db-migrator"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "admin_postgres" {
  name                 = "${var.prefix}core/admin-postgres"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_repository" "api" {
  name                 = "${var.prefix}core/api"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "custom_environments" {
  name                 = "${var.prefix}core/custom-environments"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "dashboard_api" {
  name                 = "${var.prefix}core/dashboard-api"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}

resource "aws_ecr_repository" "docker_reverse_proxy" {
  name                 = "${var.prefix}core/docker-reverse-proxy"
  image_tag_mutability = "MUTABLE"
  force_delete         = var.allow_force_destroy
  tags                 = local.resource_tags
}
