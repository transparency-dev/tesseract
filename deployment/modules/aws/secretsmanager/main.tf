terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.92.0"
    }
  }
}

# Configure the AWS Provider
provider "aws" {
  region = var.region
}

# Secrets Manager
resource "aws_secretsmanager_secret" "tesseract_ecdsa_p256_public_key" {
  name                    = "${var.base_name}-ecdsa-p256-public-key"
  recovery_window_in_days = 0

  tags = {
    label = "tesseract-public-key"
  }
}

resource "aws_secretsmanager_secret_version" "tesseract_ecdsa_p256_public_key" {
  secret_id     = aws_secretsmanager_secret.tesseract_ecdsa_p256_public_key.id
  secret_string = var.tls_private_key_ecdsa_p256_public_key_pem
}

resource "aws_secretsmanager_secret" "tesseract_ecdsa_p256_private_key" {
  name                    = "${var.base_name}-ecdsa-p256-private-key"
  recovery_window_in_days = 0

  tags = {
    label = "tesseract-private-key"
  }
}

resource "aws_secretsmanager_secret_version" "tesseract_ecdsa_p256_private_key" {
  secret_id     = aws_secretsmanager_secret.tesseract_ecdsa_p256_private_key.id
  secret_string = var.tls_private_key_ecdsa_p256_private_key_pem
}
