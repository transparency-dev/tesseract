terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.92.0"
    }
    mysql = {
      source  = "petoju/mysql"
      version = "3.0.71"
    }
  }
}

# Configure the AWS Provider
provider "aws" {
  region = var.region
}

resource "aws_s3_bucket" "log_bucket" {
  bucket = "${var.prefix_name}-${var.base_name}-bucket"

  force_destroy = var.ephemeral
}

resource "aws_rds_cluster" "log_rds_cluster" {
  cluster_identifier          = "${var.base_name}-cluster"
  engine                      = "aurora-mysql"
  engine_version              = "8.0"
  database_name               = "tesseract"
  manage_master_user_password = true
  master_username             = "tesseract"
  skip_final_snapshot         = true
  apply_immediately           = true
}

resource "aws_rds_cluster_instance" "cluster_instances" {
  count              = 1
  cluster_identifier = aws_rds_cluster.log_rds_cluster.id
  instance_class     = "db.r5.large"
  engine             = aws_rds_cluster.log_rds_cluster.engine
  engine_version     = aws_rds_cluster.log_rds_cluster.engine_version
  identifier         = "${var.base_name}-${count.index + 1}"

  force_destroy = var.ephemeral
}

# Data source to get the secret details using the ARN provided by the cluster
data "aws_secretsmanager_secret_version" "db_credentials" {
  # The secret ARN is available in the master_user_secret block (it's a list)
  secret_id = aws_rds_cluster.log_rds_cluster.master_user_secret[0].secret_arn

  depends_on = [
    aws_rds_cluster.log_rds_cluster,
    aws_rds_cluster_instance.cluster_instances
  ]
}

# Configure the MySQL provider based on the outcome of
# creating the aws_db_instance.
# This requires that the machine running OpenTofu has access
# to the DB instance created above. This is _NOT_ the case when
# GitHub actions are applying the OpenTofu.
provider "mysql" {
  endpoint = aws_rds_cluster_instance.cluster_instances[0].endpoint
  username = aws_rds_cluster.log_rds_cluster.master_username
  password = jsondecode(data.aws_secretsmanager_secret_version.db_credentials.secret_string)["password"]
}

# Create a second database for antispam.
resource "mysql_database" "antispam_db" {
  name  = "antispam_db"
  count = var.create_antispam_db ? 1 : 0
}
