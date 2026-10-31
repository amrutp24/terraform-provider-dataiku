# There is one of these per instance, so the id is ignored and any value
# works. "container_settings" is what the resource reports afterwards.
terraform import dataiku_container_settings.instance container_settings

# Importing records no previous values, so a later destroy cannot put the
# settings back the way it found them and will say so rather than guess. Only
# a resource this provider created carries that record.
