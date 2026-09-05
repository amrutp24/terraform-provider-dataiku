data "dataiku_user" "admin" {
  login = "admin"
}

output "admin_groups" {
  value = data.dataiku_user.admin.groups
}
