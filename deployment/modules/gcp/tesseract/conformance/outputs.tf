output "ecdsa_p256_public_key_id" {
  description = "Signer public key (P256_SHA256)"
  value       = module.secretmanager.ecdsa_p256_public_key_id
}

output "ecdsa_p256_public_key_data" {
  description = "Signer public key (P256_SHA256) data from secret manager"
  value       = module.secretmanager.ecdsa_p256_public_key_data
  sensitive   = true
}

output "ecdsa_p256_private_key_id" {
  description = "Signer private key (P256_SHA256)"
  value       = module.secretmanager.ecdsa_p256_private_key_id
}

output "conformance_url" {
  description = "The URL of the running conformance server"
  value       = module.cloudrun.conformance_url
}

output "conformance_bucket_name" {
  description = "The GCS bucket name of the running conformance server"
  value       = module.storage.log_bucket.name
}
