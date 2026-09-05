data "dataiku_project" "analytics" {
  project_key = "ANALYTICS"
}

output "analytics_owner" {
  value = data.dataiku_project.analytics.owner
}
