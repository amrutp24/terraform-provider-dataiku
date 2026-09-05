# End-to-end example: provision a DSS instance, then configure it.
#
# Docker is used here because it needs no cloud account, but the split is the
# point rather than the platform. Layer 1 is whatever creates the host; layer 2
# is always the same. Swap the docker_container below for a GCE instance, an
# EC2 instance, or a machine you already own, and everything under "layer 2"
# is unchanged.
#
# Apply this in two passes -- see README.md.

terraform {
  required_version = ">= 1.5"

  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = "~> 3.0"
    }
    dataiku = {
      source = "amrutp24/dataiku"
    }
  }
}

# ---------------------------------------------------------------------------
# Layer 1: the instance
# ---------------------------------------------------------------------------

provider "docker" {}

resource "docker_image" "dss" {
  name         = "dataiku/dss:${var.dss_version}"
  keep_locally = true
}

resource "docker_volume" "dss_data" {
  name = "dss-data"
}

resource "docker_container" "dss" {
  name  = "dataiku-dss"
  image = docker_image.dss.image_id

  ports {
    internal = 10000
    external = var.dss_port
    ip       = "127.0.0.1"
  }

  volumes {
    volume_name    = docker_volume.dss_data.name
    container_path = "/home/dataiku/dss"
  }

  # DSS wants plenty of file descriptors and shared memory.
  ulimit {
    name = "nofile"
    soft = 65536
    hard = 65536
  }
  shm_size = 1024

  restart = "unless-stopped"
}

# ---------------------------------------------------------------------------
# Layer 2: what is on the instance
#
# Identical whatever created the host above.
# ---------------------------------------------------------------------------

provider "dataiku" {
  host = "http://localhost:${var.dss_port}"
  # api_key comes from DATAIKU_API_KEY
}

resource "dataiku_group" "data_scientists" {
  name        = "data_scientists"
  description = "Analysts who build and run models"

  permissions = {
    mayCreateProjects  = true
    mayCreateCodeEnvs  = false
    mayWriteUnsafeCode = false
  }
}

resource "dataiku_code_env" "ml" {
  name = "ml-python"
  lang = "PYTHON"

  packages = <<-EOT
    scikit-learn==1.5.0
    pandas==2.2.2
  EOT

  install_jupyter_support = true

  # Building runs pip inside the container; turn this off if it has no
  # outbound network access.
  install_packages_on_change = var.build_code_envs
}

resource "dataiku_project" "analytics" {
  project_key = "ANALYTICS"
  name        = "Analytics"
  owner       = "admin"
  tags        = ["managed-by-terraform"]
}

resource "dataiku_project_permissions" "analytics" {
  project_key = dataiku_project.analytics.project_key

  permission {
    group                 = dataiku_group.data_scientists.name
    read_project_content  = true
    write_project_content = true
    read_dashboards       = true
    write_dashboards      = true
    run_scenarios         = true
    export_datasets_data  = true
  }
}
