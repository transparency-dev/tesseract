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
    name  = "tesseract"
    push {
      branch = "^main$"
    }
  }

  build {
    ## Install OpenTofu in alpine/terragrunt container.
    step {
      id   = "prepare_terragrunt_opentofu_container"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "terragrunt-opentofu",
        "-f", "./deployment/terragrunt-opentofu/Dockerfile",
        "."
      ]
    }

    ## Destroy any pre-existing deployment/live/gcp/static-ct/logs/ci environment.
    ## This might happen if a previous cloud build failed for some reason.
    step {
      id     = "preclean_env"
      name   = "terragrunt-opentofu"
      script = <<EOT
        terragrunt --terragrunt-non-interactive --terragrunt-no-color destroy -auto-approve -no-color 2>&1
      EOT
      dir    = "deployment/live/gcp/static-ct/logs/ci"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}"
      ]
      wait_for = ["prepare_terragrunt_opentofu_container"]
    }

    ## Build TesseraCT GCP Docker image.
    ## This will be used by the building the conformance Docker image which includes 
    ## the test data.
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

    ## Build TesseraCT GCP Conformance Docker container image.
    step {
      id   = "docker_build_conformance_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "${local.conformance_gcp_docker_image}:$SHORT_SHA",
        "-t", "${local.conformance_gcp_docker_image}:latest",
        "-f", "./cmd/tesseract/gcp/ci/Dockerfile",
        "."
      ]
      wait_for = ["docker_build_tesseract_gcp"]
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

    ## Apply the deployment/live/gcp/static-ct/logs/ci terragrunt config.
    ## This will bring up the conformance infrastructure, including a service
    ## running the conformance server docker image built above.
    step {
      id     = "terraform_apply_conformance_ci"
      name   = "terragrunt-opentofu"
      script = <<EOT
        terragrunt --terragrunt-non-interactive --terragrunt-no-color apply -auto-approve -no-color 2>&1
      EOT
      dir    = "deployment/live/gcp/static-ct/logs/ci"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}"
      ]
      wait_for = ["preclean_env", "docker_push_conformance_gcp"]
    }

    ## Print Terragrunt output to files.
    step {
      id     = "terraform_print_output"
      name   = "terragrunt-opentofu"
      script = <<EOT
        terragrunt --terragrunt-no-color output --raw tesseract_url -no-color > /workspace/conformance_url
        terragrunt --terragrunt-no-color output --raw tesseract_bucket_name -no-color > /workspace/conformance_bucket_name
        terragrunt --terragrunt-no-color output --raw ecdsa_p256_public_key_data -no-color > /workspace/conformance_log_public_key.pem
      EOT
      dir    = "deployment/live/gcp/static-ct/logs/ci"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}"
      ]
      wait_for = ["terraform_apply_conformance_ci"]
    }

    ## Since the conformance infrastructure is not publicly accessible, we need to use 
    ## bearer tokens for the test to access them.
    ## This step creates those, and stores them for later use.
    step {
      id       = "bearer_token"
      name     = "gcr.io/cloud-builders/gcloud"
      script   = <<EOT
        gcloud auth print-access-token > /workspace/cb_access
        curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/${local.cloudbuild_service_account}/identity?audience=$(cat /workspace/conformance_url)" > /workspace/cb_identity
      EOT
      wait_for = ["terraform_apply_conformance_ci"]
    }

    ## Test against the conformance server with CT Hammer.
    step {
      id       = "ct_hammer"
      name     = "golang"
      script   = <<EOT
        apt update && apt install -y retry

        openssl ec -pubin -inform PEM -in /workspace/conformance_log_public_key.pem -outform der -out /workspace/conformance_log_public_key.der
        base64 -w 0 /workspace/conformance_log_public_key.der > /workspace/conformance_log_public_key

        retry -t 5 -d 15 --until=success go run ./internal/hammer \
          --origin="ci-static-ct" \
          --log_public_key="$(cat /workspace/conformance_log_public_key)" \
          --log_url="https://storage.googleapis.com/$(cat /workspace/conformance_bucket_name)/" \
          --write_log_url="$(cat /workspace/conformance_url)/ci-static-ct" \
          -v=1 \
          --show_ui=false \
          --bearer_token="$(cat /workspace/cb_access)" \
          --bearer_token_write="$(cat /workspace/cb_identity)" \
          --logtostderr \
          --num_writers=256 \
          --max_write_ops=256 \
          --num_mmd_verifiers=256 \
          --leaf_write_goal=10000
      EOT
      wait_for = ["bearer_token"]
    }

    ## Destroy the deployment/live/gcp/static-ct/logs/ci terragrunt config.
    ## This will tear down the conformance infrastructure we brought up
    ## above.
    step {
      id     = "terraform_destroy_conformance_ci"
      name   = "terragrunt-opentofu"
      script = <<EOT
        terragrunt --terragrunt-non-interactive --terragrunt-no-color destroy -auto-approve -no-color 2>&1
      EOT
      dir    = "deployment/live/gcp/static-ct/logs/ci"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}"
      ]
      wait_for = ["ct_hammer"]
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
