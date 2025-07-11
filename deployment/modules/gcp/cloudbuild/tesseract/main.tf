terraform {
  backend "gcs" {}

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.43.0"
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
  cloudbuild_service_account   = "cloudbuild-${var.env}-sa@${var.project_id}.iam.gserviceaccount.com"
  artifact_repo                = "${var.location}-docker.pkg.dev/${var.project_id}/${module.artifactregistry.docker.name}"
  ## TODO(phbnf): this should include the name of the log since it contains roots
  tesseract_gcp_docker_image = "${local.artifact_repo}/tesseract-gcp"
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

  # TODO(phboneff): use a better mechanism to trigger releases that re-uses Docker containters, or based on branches rather.
  # This is a temporary mechanism to speed up development.
  github {
    owner = var.github_owner
    name  = "tesseract"
    push {
      tag = "^staging-deploy-(.+)$"
    }
  }

  build {
    ## Build TesseraCT GCP Docker container image, without roots.
    step {
      id   = "docker_build_tesseract_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "tesseract-gcp:$SHORT_SHA",
        "-t", "tesseract-gcp:latest",
        "-f", "./cmd/tesseract/gcp/Dockerfile",
        "."
      ]
    }

    ## Build TesseraCT GCP Docker container image, with roots.
    ## TODO(phbnf): make the docker file path a var.
    step {
      id   = "docker_build_tesseract_with_roots_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "${local.tesseract_gcp_docker_image}:$SHORT_SHA",
        "-t", "${local.tesseract_gcp_docker_image}:latest",
        "-f", "./cmd/tesseract/gcp/staging/Dockerfile",
        "."
      ]
      wait_for = ["docker_build_tesseract_gcp"]
    }

    ## Push TesseraCT's Docker container image to Artifact Registry.
    step {
      id   = "docker_push_tesseract_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "push",
        "--all-tags",
        local.tesseract_gcp_docker_image
      ]
      wait_for = ["docker_build_tesseract_with_roots_gcp"]
    }

    ## Apply the deployment/live/gcp/static-staging/logs/XXX terragrunt configs.
    ## This will bring up or update TesseraCT's infrastructure, including a service
    ## running the server docker image built above.
    dynamic "step" {
      for_each = var.logs_terragrunts
      iterator = tg_path

      content {
        id     = "terraform_apply_tesseract_${tg_path.key}"
        name   = "alpine/terragrunt"
        script = <<EOT
          terragrunt --terragrunt-non-interactive --terragrunt-no-color apply -auto-approve -no-color 2>&1
        EOT
        dir    = tg_path.value
        env = [
          "GOOGLE_PROJECT=${var.project_id}",
          "TF_IN_AUTOMATION=1",
          "TF_INPUT=false",
          "TF_VAR_project_id=${var.project_id}",
          "DOCKER_CONTAINER_TAG=$SHORT_SHA"
        ]
        wait_for = tg_path.key > 0 ? ["docker_push_tesseract_gcp", "terraform_apply_tesseract_${tg_path.key - 1}"] : ["docker_push_tesseract_gcp"]
      }
    }

    ## Print terraform output.
    dynamic "step" {
      for_each = var.logs_terragrunts
      iterator = tg_path

      content {
        id     = "terraform_print_output_${tg_path.key}"
        name   = "alpine/terragrunt"
        script = <<EOT
          terragrunt --terragrunt-no-color output --raw tesseract_url -no-color > /workspace/tesseract_url
          terragrunt --terragrunt-no-color output --raw tesseract_bucket_name -no-color > /workspace/tesseract_bucket_name
          terragrunt --terragrunt-no-color output --raw ecdsa_p256_public_key_data -no-color > /workspace/tesseract_log_public_key.pem
          cat /workspace/tesseract_url
          cat /workspace/tesseract_bucket_name
          cat /workspace/tesseract_log_public_key.pem
        EOT
        dir    = tg_path.value
        env = [
          "GOOGLE_PROJECT=${var.project_id}",
          "TF_IN_AUTOMATION=1",
          "TF_INPUT=false",
          "TF_VAR_project_id=${var.project_id}",
          "DOCKER_CONTAINER_TAG=$SHORT_SHA"
        ]
        wait_for = ["terraform_apply_tesseract_${tg_path.key}"]
      }
    }

    options {
      logging      = "CLOUD_LOGGING_ONLY"
      machine_type = "E2_HIGHCPU_8"
    }
  }

  depends_on = [
    module.artifactregistry
  ]
}
