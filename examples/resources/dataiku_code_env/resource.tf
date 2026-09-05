# The environment data scientists select when writing Python recipes and
# notebooks. Building it runs pip on the instance, so the instance needs
# outbound access to a package index.
resource "dataiku_code_env" "ml" {
  name = "ml-python311"
  lang = "PYTHON"

  python_interpreter = "PYTHON311"

  packages = <<-EOT
    scikit-learn==1.5.0
    pandas==2.2.2
    xgboost==2.1.1
  EOT

  install_jupyter_support = true
}

# Manage the definition without letting Terraform trigger the build, for
# instances with no outbound network access or where builds are run separately.
resource "dataiku_code_env" "offline" {
  name = "offline-env"
  lang = "PYTHON"

  packages                   = "requests==2.32.3"
  install_packages_on_change = false
}
