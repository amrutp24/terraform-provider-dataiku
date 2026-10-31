# Execution configurations are imported by name.
terraform import dataiku_container_execution_config.gke gke-exec

# As with the build configuration, settings_json is seeded empty on import
# because the instance cannot tell you which keys you set and which DSS
# supplied. Read effective_json first, then write settings_json to match the
# parts you mean to manage.
