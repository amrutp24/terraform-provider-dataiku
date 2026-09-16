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

# An environment for containerized execution. A new environment is built for
# every container configuration on the instance; this narrows it to one.
#
# The list is only consulted while all_container_configs is false. DSS stores
# it either way and keeps building for everything, so a list on its own looks
# like it took effect and did not.
resource "dataiku_code_env" "containerized" {
  name = "container-py312"
  lang = "PYTHON"

  # Match the interpreter to the base image. DSS 15 otherwise falls back to
  # python3.9, which an Ubuntu 24.04 image does not have, and the build fails
  # after the environment is reported as created.
  python_interpreter = "PYTHON312"

  packages = "requests==2.32.3"

  all_container_configs = false
  container_configs     = [dataiku_container_execution_config.gke.name]
}

resource "dataiku_container_execution_config" "gke" {
  name = "gke-exec"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = "gke-build"
    usableBy         = "ALL"
    allowedGroups    = []
  })
}
