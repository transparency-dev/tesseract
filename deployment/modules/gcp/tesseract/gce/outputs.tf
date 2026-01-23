output "tesseract_url" {
  description = "The submission URL of the running TesseraCT server"
  value       = module.ilb.tesseract_url
}

output "tesseract_bucket_name" {
  description = "The GCS bucket name of the TesseraCT log"
  value       = module.storage.log_bucket.name
}
