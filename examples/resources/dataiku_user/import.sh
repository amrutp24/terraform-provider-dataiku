# Users are imported by their login. The password is never returned by DSS, so
# it stays null in state after an import.
terraform import dataiku_user.jsmith jsmith
