output "ecdsa_p256_public_key_id" {
  description = "Signer public key (P256_SHA256)"
  value       = data.google_secret_manager_secret_version.tesseract_ecdsa_p256_public_key.id
}

output "ecdsa_p256_public_key_data" {
  description = "Signer public key (P256_SHA256) data from secret manager"
  value       = data.google_secret_manager_secret_version.tesseract_ecdsa_p256_public_key.secret_data
  sensitive   = true
}

output "ecdsa_p256_private_key_id" {
  description = "Signer private key (P256_SHA256)"
  value       = data.google_secret_manager_secret_version.tesseract_ecdsa_p256_private_key.id
}
