resource "dataiku_project_folder" "analytics" {
  name = "Analytics"
}

resource "dataiku_project_folder" "reporting" {
  name      = "Reporting"
  parent_id = dataiku_project_folder.analytics.id
}

# DSS assigns folder ids, so reference the attribute rather than writing one.
resource "dataiku_project" "weekly" {
  project_key       = "WEEKLY"
  name              = "Weekly report"
  owner             = "admin"
  project_folder_id = dataiku_project_folder.reporting.id
}
