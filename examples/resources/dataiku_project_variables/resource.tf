# This resource is authoritative over both scopes: it replaces the project's
# whole variable document.
resource "dataiku_project_variables" "analytics" {
  project_key = dataiku_project.analytics.project_key

  # Standard variables travel with a project bundle.
  standard = jsonencode({
    reporting_currency = "USD"
    lookback_days      = 90
  })

  # Local variables stay on this instance, which makes them the right place
  # for per-environment values and credentials.
  local = jsonencode({
    environment   = "production"
    warehouse_dsn = var.warehouse_dsn
  })
}
