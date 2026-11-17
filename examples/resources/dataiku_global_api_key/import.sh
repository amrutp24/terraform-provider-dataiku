# Keys are imported by their id, not by their secret or their label. The id is
# the "id" field of each entry that GET /public/api/admin/global-api-keys/
# returns.
#
# An imported key has no secret in state: DSS gives it out once, when the key
# is created, and masks it on every read after that. Abilities are tracked
# only for the names the configuration's permissions map lists.
terraform import dataiku_global_api_key.ci Ab3dEf6hIj9kLm2n
