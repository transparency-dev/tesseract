terraform {
  backend "gcs" {}

  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

# Artifact Registry

module "artifactregistry" {
  source = "../../artifactregistry"

  location   = var.location
  docker_env = var.docker_env
}

# Cloud Build

locals {
  cloudbuild_service_account   = "cloudbuild-sa@${var.project_id}.iam.gserviceaccount.com"
  artifact_repo                = "${var.location}-docker.pkg.dev/${var.project_id}/${module.artifactregistry.docker.name}"
  conformance_gcp_docker_image = "${local.artifact_repo}/conformance-gcp"
}

resource "google_project_service" "cloudbuild_api" {
  service            = "cloudbuild.googleapis.com"
  disable_on_destroy = false
}

## Service usage API is required on the project to enable APIs.
## https://cloud.google.com/apis/docs/getting-started#enabling_apis
## serviceusage.googleapis.com acts as a central point for managing the API 
## lifecycle within your project. By ensuring the required APIs are enabled 
## and accessible, it allows Cloud Build to function seamlessly and interact 
## with other Google Cloud services as needed.
## 
## The Cloud Build service account also needs roles/serviceusage.serviceUsageViewer.
resource "google_project_service" "serviceusage_api" {
  service            = "serviceusage.googleapis.com"
  disable_on_destroy = false
}

resource "google_cloudbuild_trigger" "build_trigger" {
  name            = "build-docker-${var.docker_env}"
  service_account = "projects/${var.project_id}/serviceAccounts/${local.cloudbuild_service_account}"
  location        = var.location

  github {
    owner = var.github_owner
    name  = "static-ct"
  }

  build {
    ## Build the SCTFE GCP Docker image.
    ## This will be used by the building the conformance Docker image which includes 
    ## the test data.
    step {
      id   = "docker_build_sctfe_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "sctfe-gcp:$SHORT_SHA",
        "-t", "sctfe-gcp:latest",
        "-f", "./cmd/gcp/Dockerfile",
        "."
      ]
    }

    ## Build the SCTFE GCP Conformance Docker container image.
    step {
      id   = "docker_build_conformance_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "${local.conformance_gcp_docker_image}:$SHORT_SHA",
        "-t", "${local.conformance_gcp_docker_image}:latest",
        "-f", "./cmd/gcp/staging/Dockerfile",
        "."
      ]
      wait_for = ["docker_build_sctfe_gcp"]
    }

    ## Push the conformance Docker container image to Artifact Registry.
    step {
      id   = "docker_push_conformance_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "push",
        "--all-tags",
        local.conformance_gcp_docker_image
      ]
      wait_for = ["docker_build_conformance_gcp"]
    }

    ## Apply the deployment/live/gcp/static-ct-staging/logs/arche2025h1 terragrunt config.
    ## This will bring up the conformance infrastructure, including a service
    ## running the conformance server docker image built above.
    step {
      id     = "terraform_apply_arche2025h1"
      name   = "alpine/terragrunt"
      script = <<EOT
        terragrunt --terragrunt-non-interactive --terragrunt-no-color apply -auto-approve -no-color 2>&1
        terragrunt --terragrunt-no-color output --raw conformance_url -no-color > /workspace/log_url
        terragrunt --terragrunt-no-color output --raw ecdsa_p256_public_key_data -no-color > /workspace/arche2025h1_log_public_key.pem
      EOT
      dir    = "deployment/live/gcp/static-ct-staging/logs/arch2025h1"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}"
      ]
      wait_for = ["preclean_env", "docker_push_conformance_gcp"]
    }
}
