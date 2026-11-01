# Image build configurations are imported by name.
terraform import dataiku_container_image_build_config.gke gke-build

# settings_json is your intent and cannot be recovered from the instance,
# which holds it merged with every default DSS added. Import seeds it empty,
# so the first plan shows what your configuration would set. Read
# effective_json to see what is actually there, and build settings_json from
# that before applying.
