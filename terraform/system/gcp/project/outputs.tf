output "services" {
  depends_on = [
    google_project_service.cloudresourcemanager,
    google_project_service.compute,
    google_project_service.container,
    google_project_service.iam,
    google_project_service.redis,
  ]

  value = "services"
}
