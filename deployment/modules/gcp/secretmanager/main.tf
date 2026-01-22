terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.50.0"
    }
  }
}

# Secret Manager

resource "google_project_service" "secretmanager_googleapis_com" {
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

data "google_secret_manager_secret" "tesseract_ecdsa_p256_public_key" {
  secret_id  = "${var.base_name}${var.public_key_suffix}"
  depends_on = [google_project_service.secretmanager_googleapis_com]
}

data "google_secret_manager_secret_version" "tesseract_ecdsa_p256_public_key" {
  secret            = data.google_secret_manager_secret.tesseract_ecdsa_p256_public_key.id
  fetch_secret_data = false
}

data "google_secret_manager_secret" "tesseract_ecdsa_p256_private_key" {
  secret_id  = "${var.base_name}${var.private_key_suffix}"
  depends_on = [google_project_service.secretmanager_googleapis_com]
}

data "google_secret_manager_secret_version" "tesseract_ecdsa_p256_private_key" {
  secret            = data.google_secret_manager_secret.tesseract_ecdsa_p256_private_key.id
  fetch_secret_data = false
}
