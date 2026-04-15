output "tesseract_url" {
  description = "The submission URL of the running TesseraCT server"
  value       = var.create_internal_load_balancer ? module.ilb[0].tesseract_url : "No private URL configured."
}

output "tesseract_bucket_name" {
  description = "The GCS bucket name of the TesseraCT log"
  value       = module.storage.log_bucket.name
}
