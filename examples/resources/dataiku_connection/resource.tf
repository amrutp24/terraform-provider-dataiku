resource "dataiku_connection" "warehouse" {
  name = "warehouse"
  type = "PostgreSQL"

  description = "Analytics read replica"

  # params_json_wo keeps the credentials out of Terraform state. Bump the
  # version marker when they change. Needs Terraform 1.11+; use params_json
  # instead on older versions, accepting that it is persisted.
  params_json_wo = jsonencode({
    host     = "db.example.com"
    port     = "5432"
    db       = "analytics"
    user     = "dataiku"
    password = var.warehouse_password
  })
  params_json_wo_version = "1"

  usable_by      = "ALLOWED"
  allowed_groups = [dataiku_group.data_scientists.name]
}

# Amazon S3 connections use the type "EC2", for historical reasons. There is no
# "S3" type; asking for one fails with an opaque internal error.
resource "dataiku_connection" "datalake" {
  name = "datalake"
  type = "EC2"

  params_json = jsonencode({
    credentialsMode      = "ENVIRONMENT"
    defaultManagedBucket = "example-datalake"
    defaultManagedPath   = "/dataiku/managed"
  })
}
