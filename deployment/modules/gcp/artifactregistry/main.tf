terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.43.0"
    }
  }
}

# Artifact Registry

resource "google_project_service" "artifact_registry_api" {
  service            = "artifactregistry.googleapis.com"
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "docker" {
  repository_id = "docker-${var.docker_env}"
  location      = var.location
  description   = "Static CT Docker images"
  format        = "DOCKER"
  depends_on = [
    google_project_service.artifact_registry_api,
  ]
}
