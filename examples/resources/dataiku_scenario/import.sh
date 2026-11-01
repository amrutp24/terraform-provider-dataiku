# Scenarios are imported as "<project_key>/<scenario_id>". For a scenario
# created through the API, and so through this provider, the id is derived
# from the name by replacing anything that is not a letter, digit, dot or
# dash with an underscore: "Nightly build" becomes "Nightly_build".
terraform import dataiku_scenario.nightly ANALYTICS/Nightly_build

# Do not derive the id for a scenario somebody made in the DSS UI. The UI
# also uppercases, where the API does not: a scenario named "test1" in the
# browser has the id "TEST1", which the rule above would not produce. Read
# the id rather than working it out, either from the scenario's page in DSS,
# where it is in the URL, or from GET /public/api/projects/ANALYTICS/scenarios/.
