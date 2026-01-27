terraform {
  backend "s3" {}
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.92.0"
    }
  }
}

locals {
  name = "${var.prefix_name}-${var.base_name}"
  port = 6962
}

module "storage" {
  source = "../../storage"

  prefix_name        = var.prefix_name
  base_name          = var.base_name
  region             = var.region
  ephemeral          = var.ephemeral
  create_antispam_db = var.create_antispam_db
}

module "secretsmanager" {
  source = "../../secretsmanager"

  base_name                                  = var.base_name
  region                                     = var.region
  tls_private_key_ecdsa_p256_public_key_pem  = module.insecuretlskey.tls_private_key_ecdsa_p256_public_key_pem
  tls_private_key_ecdsa_p256_private_key_pem = module.insecuretlskey.tls_private_key_ecdsa_p256_private_key_pem
}

# [WARNING]
# This module will store unencrypted private keys in the OpenTofu state file.
# DO NOT use this for production logs.
module "insecuretlskey" {
  source = "../../insecuretlskey"
}

# Virtual private cloud
# This will be used for the containers to communicate between themselves, and
# the S3 bucket.
resource "aws_default_vpc" "default" {
  tags = {
    Name = "Default VPC"
  }
}

data "aws_subnets" "subnets" {
  filter {
    name   = "vpc-id"
    values = [aws_default_vpc.default.id]
  }
}

## Service discovery ###########################################################
# This will be used by the hammer to contact multiple conformance tasks with a single
# dns name.
resource "aws_service_discovery_private_dns_namespace" "internal" {
  name = "internal"
  vpc  = aws_default_vpc.default.id
}

resource "aws_service_discovery_service" "conformance" {
  name = "conformance"

  dns_config {
    namespace_id = aws_service_discovery_private_dns_namespace.internal.id

    dns_records {
      ttl  = 10
      type = "A"
    }

    routing_policy = "MULTIVALUE"
  }

  health_check_custom_config {
    failure_threshold = 1
  }
}

# S3 Gateway Endpoint (connects via route table)
resource "aws_vpc_endpoint" "s3" {
  vpc_id       = aws_default_vpc.default.id
  service_name = "com.amazonaws.${var.region}.s3"
}

resource "aws_vpc_endpoint_route_table_association" "private_s3" {
  vpc_endpoint_id = aws_vpc_endpoint.s3.id
  route_table_id  = aws_default_vpc.default.default_route_table_id
}

resource "aws_s3_bucket_policy" "allow_access_from_vpce" {
  bucket = module.storage.s3_bucket_name
  policy = data.aws_iam_policy_document.allow_access_from_vpce.json
}

data "aws_iam_policy_document" "allow_access_from_vpce" {
  statement {
    principals {
      type        = "*"
      identifiers = ["*"]
    }

    actions = [
      "s3:GetObject",
    ]

    resources = [
      "${module.storage.s3_bucket_arn}/*",
    ]

    condition {
      test     = "StringEquals"
      variable = "aws:sourceVpce"
      values   = [aws_vpc_endpoint.s3.id]
    }
  }
  depends_on = [aws_vpc_endpoint.s3]
}

# ECS cluster
# This will be used to run the conformance and hammer binaries on Fargate.
resource "aws_ecs_cluster" "ecs_cluster" {
  name = local.name
}

resource "aws_ecs_cluster_capacity_providers" "ecs_capacity" {
  cluster_name = aws_ecs_cluster.ecs_cluster.name

  capacity_providers = ["FARGATE"]
}

# Conformance task and service
# This will start multiple conformance tasks on Fargate within a service.
resource "aws_ecs_task_definition" "conformance" {
  family                   = "conformance"
  requires_compatibilities = ["FARGATE"]
  # Required network_mode for tasks running on Fargate.
  network_mode = "awsvpc"
  cpu          = 1024
  memory       = 2048
  # ARN of the task execution role that the Amazon ECS container agent and the Docker daemon can assume.
  execution_role_arn = var.ecs_execution_role
  # ARN of IAM role that allows your Amazon ECS container task to make calls to other AWS services.
  task_role_arn = var.ecs_conformance_task_role
  container_definitions = jsonencode([{
    "name" : "${local.name}-conformance",
    "image" : "${var.ecr_registry}/${var.ecr_repository_conformance}",
    "cpu" : 0,
    "portMappings" : [{
      "name" : "conformance-${local.port}-tcp",
      "containerPort" : local.port,
      "hostPort" : local.port,
      "protocol" : "tcp",
      "appProtocol" : "http"
    }],
    "essential" : true,
    "command" : [
      "--http_endpoint=:${local.port}",
      "--roots_pem_file=/bin/test_root_ca_cert.pem",
      "--origin=ci-static-ct",
      "--path_prefix=ci-static-ct",
      "--bucket=${module.storage.s3_bucket_name}",
      "--db_user=tesseract",
      "--db_password=${module.storage.rds_aurora_cluster_master_user_secret_unsafe}",
      "--db_name=tesseract",
      "--db_host=${module.storage.rds_aurora_cluster_endpoint}",
      "--db_port=3306",
      "--signer_public_key_secret_name=${module.secretsmanager.ecdsa_p256_public_key_id}",
      "--signer_private_key_secret_name=${module.secretsmanager.ecdsa_p256_private_key_id}",
      "--antispam_db_name=${var.antispam_database_name}",
      "--inmemory_antispam_cache_size=256k",
      "--enable_publication_awaiter=true",
      "--roots_remote_fetch_url=${var.roots_remote_fetch_url}",
      "--roots_remote_fetch_interval=${var.roots_remote_fetch_interval}",
      "-v=2"
    ],
    "logConfiguration" : {
      "logDriver" : "awslogs",
      "options" : {
        "awslogs-group" : "/ecs/${local.name}",
        "mode" : "non-blocking",
        "awslogs-create-group" : "true",
        "max-buffer-size" : "25m",
        "awslogs-region" : var.region,
        "awslogs-stream-prefix" : "ecs"
      },
    },
  }])

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }

  depends_on = [module.storage]
}

resource "aws_ecs_service" "conformance_service" {
  name                  = local.name
  task_definition       = aws_ecs_task_definition.conformance.arn
  cluster               = aws_ecs_cluster.ecs_cluster.arn
  launch_type           = "FARGATE"
  desired_count         = 3
  wait_for_steady_state = true

  network_configuration {
    subnets = data.aws_subnets.subnets.ids
    # required to access container registry
    assign_public_ip = true
  }

  # connect the service with the service discovery defined above
  service_registries {
    registry_arn = aws_service_discovery_service.conformance.arn
  }

  depends_on = [
    aws_service_discovery_private_dns_namespace.internal,
    aws_service_discovery_service.conformance,
    aws_ecs_cluster.ecs_cluster,
    aws_ecs_task_definition.conformance,
  ]
}

# Hammer task definition and execution
# The hammer can also be launched manually with the following command: 
# aws ecs run-task \
#   --cluster="$(terragrunt output -raw ecs_cluster)" \
#   --task-definition=hammer \
#   --count=1 \
#   --launch-type=FARGATE \
#   --network-configuration='{"awsvpcConfiguration": {"assignPublicIp":"ENABLED","subnets": '$(terragrunt output -json vpc_subnets)'}}'
resource "aws_ecs_task_definition" "hammer" {
  family                   = "hammer"
  requires_compatibilities = ["FARGATE"]
  # Required network_mode for tasks running on Fargate
  network_mode       = "awsvpc"
  cpu                = 1024
  memory             = 2048
  execution_role_arn = var.ecs_execution_role
  container_definitions = jsonencode([{
    "name" : "${local.name}-hammer",
    "image" : "${var.ecr_registry}/${var.ecr_repository_hammer}",
    "cpu" : 0,
    "essential" : true,
    "command" : [
      "--origin=ci-static-ct",
      "--log_public_key=${module.insecuretlskey.tls_private_key_ecdsa_p256_public_key_base64_der}",
      "--intermediate_ca_cert_path=/bin/test_intermediate_ca_cert.pem",
      "--intermediate_ca_key_path=/bin/test_intermediate_ca_private_key.pem",
      "--cert_sign_private_key_path=/bin/test_leaf_cert_signing_private_key.pem",
      "--log_url=https://${module.storage.s3_bucket_regional_domain_name}",
      "--write_log_url=http://${aws_service_discovery_service.conformance.name}.${aws_service_discovery_private_dns_namespace.internal.name}:${local.port}/ci-static-ct",
      "--show_ui=false",
      "--logtostderr",
      "--num_writers=256",
      "--max_write_ops=256",
      "--num_mmd_verifiers=1024",
      "--leaf_write_goal=10000",
      "-v=3",
    ],
    "logConfiguration" : {
      "logDriver" : "awslogs",
      "options" : {
        "awslogs-group" : "/ecs/${local.name}-hammer",
        "mode" : "non-blocking",
        "awslogs-create-group" : "true",
        "max-buffer-size" : "25m",
        "awslogs-region" : var.region,
        "awslogs-stream-prefix" : "ecs"
      },
    },
  }])

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }

  depends_on = [
    module.storage,
    aws_ecs_cluster.ecs_cluster,
  ]
}
