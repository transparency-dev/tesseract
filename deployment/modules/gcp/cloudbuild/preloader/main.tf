terraform {
  backend "gcs" {}

  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

# Cloud Build

locals {
  cloudbuild_service_account   = "cloudbuild-${var.env}-sa@${var.project_id}.iam.gserviceaccount.com"
  scheduler_service_account    = "scheduler-${var.env}-sa@${var.project_id}.iam.gserviceaccount.com"
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

resource "google_cloudbuild_trigger" "preloader_trigger" {
  name            = "preloader-${var.base_name}"
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
    ## Since TesseraCT's infrastructure is not publicly accessible, we need to use 
    ## bearer tokens for the test to access them.
    ## This step creates those, and stores them for later use.
    step {
      id       = "bearer_token"
      name     = "gcr.io/cloud-builders/gcloud"
      script   = <<EOT
        gcloud auth print-access-token > /workspace/cb_access
        curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/${local.cloudbuild_service_account}/identity?audience=${var.submission_url}" > /workspace/cb_identity
      EOT
    }

    ## TODO(phboneff): move to its own container / cloudrun / batch job.
    ## Preload entries.
    ## Leave enough time for the preloader to run, until the token expires.
    ## Stop after 360k entries, this is what gets copied within 60 minutes.
    timeout = "4200s" // 60 minutes
    step {
      id       = "ct_preloader"
      name     = "golang"
      script   = <<EOT
	      START_INDEX=$(($(curl -H "Authorization: Bearer $(cat /workspace/cb_access)" ${var.monitoring_url}/checkpoint | head -2 | tail -1)+${var.start_index_offset}))
	      END_INDEX=$(($START_INDEX+360000))
	      echo "Will run preloader between $START_INDEX and $END_INDEX"
        go run github.com/google/certificate-transparency-go/preload/preloader@master \
          --target_log_uri=${var.submission_url}/ \
          --target_bearer_token="$(cat /workspace/cb_identity)" \
          --source_log_uri=${var.source_log_uri} \
          --start_index=$START_INDEX \
          --end_index=$END_INDEX \
          --num_workers=20 \
          --parallel_fetch=20 \
          --parallel_submit=20
      EOT
      wait_for = ["bearer_token"]
      timeout = "3420s" // 57 minutes, since token validity if of 60 min.
    }

    options {
      logging      = "CLOUD_LOGGING_ONLY"
      machine_type = "E2_HIGHCPU_8"
    }
  }
}

// TODO(phboneff): replace with a long running job once the log is public.
resource "google_cloud_scheduler_job" "deploy_cron" {
  paused = false
  project = var.project_id
  region  = var.location
  name    = "deploy-cron-${var.base_name}"

  schedule  = "50 * * * *"
  time_zone = "America/Los_Angeles"

  attempt_deadline = "120s"

  http_target {
    http_method = "POST"
    uri         = "https://cloudbuild.googleapis.com/v1/projects/${var.project_id}/locations/${var.location}/triggers/${google_cloudbuild_trigger.preloader_trigger.trigger_id}:run"
    body        = base64encode(jsonencode({
      source = {
        branchName = "main"
      }
    }))
    headers = {
      "Content-Type" = "application/json"
    }

    oauth_token {
      service_account_email = local.scheduler_service_account
    }
  }
}
