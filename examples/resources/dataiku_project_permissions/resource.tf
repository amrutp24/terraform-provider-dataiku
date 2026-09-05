# This resource is authoritative: it replaces every group grant on the project.
resource "dataiku_project_permissions" "analytics" {
  project_key = dataiku_project.analytics.project_key

  permission {
    group                 = dataiku_group.data_scientists.name
    read_project_content  = true
    write_project_content = true
    read_dashboards       = true
    write_dashboards      = true
    run_scenario          = true
    export_datasets_data  = true
  }

  permission {
    group           = "readers"
    read_dashboards = true
  }
}
