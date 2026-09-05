data "dataiku_connection" "warehouse" {
  name = "warehouse"
}

output "warehouse_type" {
  value = data.dataiku_connection.warehouse.type
}
