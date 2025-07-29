output "instance_group" {
  description = "The full URL of the instance group created by the manager."
  value       = google_compute_region_instance_group_manager.instance_group_manager.instance_group
}
