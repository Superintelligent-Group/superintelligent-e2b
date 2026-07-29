module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.5.3"

  name = "${var.prefix}vpc"
  cidr = var.vpc_cidr

  azs                 = var.vpc_availability_zones
  public_subnets      = var.vpc_public_subnets
  private_subnets     = var.vpc_private_subnets
  elasticache_subnets = var.vpc_elasticache_subnets

  elasticache_subnet_assign_ipv6_address_on_creation                = false
  elasticache_subnet_enable_resource_name_dns_aaaa_record_on_launch = false
  elasticache_subnet_enable_dns64                                   = false

  create_database_subnet_group           = false
  create_database_subnet_route_table     = false
  create_database_internet_gateway_route = false

  manage_default_security_group = false
  manage_default_route_table    = false
  manage_default_network_acl    = false

  enable_dns_support   = true
  enable_dns_hostnames = true

  single_nat_gateway = true // share NAT Gateway for all private subnets, otherwise it will create NAT per AZ
  enable_nat_gateway = true

  map_public_ip_on_launch = false
}

data "aws_subnet" "default_private" {
  for_each   = toset(var.vpc_private_subnets)
  vpc_id     = module.vpc.vpc_id
  cidr_block = each.value
  depends_on = [
    module.vpc
  ]
}

data "aws_subnet" "default_public" {
  for_each   = toset(var.vpc_public_subnets)
  vpc_id     = module.vpc.vpc_id
  cidr_block = each.value
  depends_on = [
    module.vpc
  ]
}

locals {
  default_private_subnet_ids = [
    for subnet in data.aws_subnet.default_private : subnet.id
  ]

  default_public_subnet_ids = [
    for subnet in data.aws_subnet.default_public : subnet.id
  ]
}

resource "aws_security_group" "vpc_endpoint" {
  name        = "${var.prefix}vpc-endpoint"
  description = "Allow traffic to AWS VPC endpoints"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Allow HTTPS traffic to AWS VPC endpoints"
    from_port       = 443
    to_port         = 443
    protocol        = "TCP"
    security_groups = var.vpc_endpoint_ingress_subnet_ids
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.prefix}vpc-endpoint"
  }
}

module "vpc_endpoints" {
  source  = "terraform-aws-modules/vpc/aws//modules/vpc-endpoints"
  version = "5.5.3"

  vpc_id = module.vpc.vpc_id
  security_group_ids = [
    aws_security_group.vpc_endpoint.id
  ]

  endpoints = {
    s3 = {
      service         = "s3"
      service_type    = "Gateway"
      route_table_ids = module.vpc.private_route_table_ids
      tags = {
        Name = "${var.prefix}s3-vpc-endpoint"
      }
    },

    secrets_manager = {
      service      = "secretsmanager"
      service_type = "Interface" // gateway endpoint not supported
      subnet_ids = [
        local.default_private_subnet_ids[0],
        local.default_private_subnet_ids[1],
        local.default_private_subnet_ids[2],
      ]
      tags = {
        Name = "${var.prefix}secrets-manager-vpc-endpoint"
      }
    },

    ec2 = {
      service      = "ec2"
      service_type = "Interface"
      subnet_ids = [
        local.default_private_subnet_ids[0],
        local.default_private_subnet_ids[1],
        local.default_private_subnet_ids[2],
      ],
      tags = {
        Name = "${var.prefix}ec2-vpc-endpoint"
      }
    },

    # SUP-644: ECS/Nomad task image pulls and log shipping were still routing
    # through NAT — the same class of leak as the vpc-dev S3 gap (SIG's product
    # repo, same issue). ECR needs BOTH api and dkr; Logs covers CloudWatch Logs
    # driver traffic.
    ecr_api = {
      service      = "ecr.api"
      service_type = "Interface"
      subnet_ids = [
        local.default_private_subnet_ids[0],
        local.default_private_subnet_ids[1],
        local.default_private_subnet_ids[2],
      ],
      tags = {
        Name = "${var.prefix}ecr-api-vpc-endpoint"
      }
    },

    ecr_dkr = {
      service      = "ecr.dkr"
      service_type = "Interface"
      subnet_ids = [
        local.default_private_subnet_ids[0],
        local.default_private_subnet_ids[1],
        local.default_private_subnet_ids[2],
      ],
      tags = {
        Name = "${var.prefix}ecr-dkr-vpc-endpoint"
      }
    },

    logs = {
      service      = "logs"
      service_type = "Interface"
      subnet_ids = [
        local.default_private_subnet_ids[0],
        local.default_private_subnet_ids[1],
        local.default_private_subnet_ids[2],
      ],
      tags = {
        Name = "${var.prefix}logs-vpc-endpoint"
      }
    }
  }
}

# The stable, already-created VPC (module.vpc's own aws_vpc.this) -- looked
# up by id rather than referenced as module.vpc.vpc_id so that these new
# resources don't pull module.vpc's entire resource set into any `-target`
# plan/apply of this peering setup. (This VPC's config vs. state has an
# unrelated, pre-existing tag-prefix drift -- dev.cq.tfvars's prefix lost
# its trailing "-" at some point after the original apply -- that must not
# get swept into this change.)
data "aws_vpc" "self" {
  id = "vpc-0b787fd9230a01e00"
}

data "aws_nat_gateway" "self" {
  vpc_id = data.aws_vpc.self.id
  state  = "available"
}

resource "aws_vpc_ipv4_cidr_block_association" "peering" {
  vpc_id     = data.aws_vpc.self.id
  cidr_block = var.vpc_peering_cidr
}

resource "aws_subnet" "peering" {
  for_each = var.vpc_peering_subnets

  vpc_id            = data.aws_vpc.self.id
  cidr_block        = each.value
  availability_zone = each.key

  depends_on = [aws_vpc_ipv4_cidr_block_association.peering]

  tags = {
    Name = "${var.prefix}vpc-peering-${each.key}"
  }
}

# Dedicated route table (not the shared private one, which already has a
# "local" route claiming all of 10.0.0.0/16 -- adding a route for vpc-dev's
# 10.0.0.0/16 there would collide with it). NAT egress is preserved so
# these subnets keep general internet access (image pulls, package installs).
resource "aws_route_table" "peering" {
  vpc_id = data.aws_vpc.self.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = data.aws_nat_gateway.self.id
  }

  tags = {
    Name = "${var.prefix}vpc-peering-rt"
  }
}

resource "aws_route_table_association" "peering" {
  for_each = aws_subnet.peering

  subnet_id      = each.value.id
  route_table_id = aws_route_table.peering.id
}

resource "aws_ec2_instance_connect_endpoint" "connect" {
  // Deploy only if enabled
  count = var.use_instance_connect ? 1 : 0

  preserve_client_ip = false
  subnet_id          = module.vpc.private_subnets[0]
  security_group_ids = [
    aws_security_group.connect_endpoint[0].id
  ]

  tags = {
    Name = "${var.prefix}instance-connect-vpc-endpoint"
  }
}

// ingress rule for SSH access is not needed there because AWS Instance Connect
resource "aws_security_group" "connect_endpoint" {
  // Deploy only if enabled
  count = var.use_instance_connect ? 1 : 0

  name        = "${var.prefix}instance-connect-endpoint"
  description = "Allow EC2 Instance SSH Connect Access"
  vpc_id      = module.vpc.vpc_id

  egress {
    from_port = 22
    to_port   = 22
    protocol  = "tcp"
    cidr_blocks = [
      module.vpc.vpc_cidr_block
    ]
  }

  tags = {
    Name = "${var.prefix}instance-connect-endpoint"
  }
}
