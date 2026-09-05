resource "dataiku_connection" "warehouse" {
  name = "warehouse"
  type = "PostgreSQL"

  description = "Analytics read replica"

  params_json = jsonencode({
    host     = "db.example.com"
    port     = "5432"
    db       = "analytics"
    user     = "dataiku"
    password = var.warehouse_password
  })

  usable_by      = "ALLOWED"
  allowed_groups = [dataiku_group.data_scientists.name]
}

resource "dataiku_connection" "datalake" {
  name = "datalake"
  type = "S3"

  params_json = jsonencode({
    credentialsMode      = "ENVIRONMENT"
    defaultManagedBucket = "example-datalake"
    defaultManagedPath   = "/dataiku/managed"
  })
}
