# Connections are imported by name. DSS redacts secret parameters, so review
# params_json after importing and re-supply any credentials.
terraform import dataiku_connection.warehouse warehouse
