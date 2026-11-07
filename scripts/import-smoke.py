"""End-to-end import check against a real DSS instance.

The acceptance suite's import steps run in process, through the plugin
protocol. They do not run the terraform import command, and they do not plan
afterwards, which is how a dirty import survived a green suite: the project's
three clear_*_on_delete flags were in ImportStateVerifyIgnore.

This creates objects on the instance, imports each one with the real CLI, and
requires that the next plan is empty. An empty plan is the whole assertion.

Needs DATAIKU_HOST and DATAIKU_API_KEY, and a built provider binary in the
repo root for the dev_overrides block to point at.
"""

import base64
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

HOST = os.environ["DATAIKU_HOST"].rstrip("/")
KEY = os.environ["DATAIKU_API_KEY"]
# The repository root, which is where the built provider binary lives for the
# dev_overrides block below. Derived from this file so the script works from
# any checkout; override if your binary is somewhere else.
REPO = os.environ.get(
    "DATAIKU_PROVIDER_DIR",
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
)
WORK = os.environ.get("IMPORT_SMOKE_DIR", os.path.join(os.path.dirname(__file__), "smoke"))

AUTH = "Basic " + base64.b64encode((KEY + ":").encode()).decode()
PROJECT = "IMPORTSMOKE"
GROUP = "smokegroup"
USER = "smokeuser"
CONN = "smokeconn"


def api(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(HOST + "/public/api" + path, data=data, method=method)
    req.add_header("Authorization", AUTH)
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw.strip() else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw[:200]


def tf(*args, check=True):
    env = dict(os.environ)
    env["TF_CLI_CONFIG_FILE"] = os.path.join(WORK, "dev.tfrc")
    p = subprocess.run(
        ["terraform", "-chdir=" + WORK] + list(args),
        capture_output=True, text=True, env=env,
    )
    if check and p.returncode != 0:
        print(p.stdout)
        print(p.stderr, file=sys.stderr)
        raise SystemExit("terraform %s failed" % (args,))
    return p


def licensed_profile():
    """Ask the instance. A profile it does not licence is stored by DSS and
    then breaks every later project create and user delete, permanently."""
    st, status = api("GET", "/admin/licensing/status")
    if st != 200 or not isinstance(status, dict):
        raise SystemExit("could not read licensing status: http=%s" % st)
    profiles = sorted((status.get("limits") or {}).get("licensedProfiles") or {})
    if not profiles:
        raise SystemExit("the instance licences no user profiles; nothing safe to create")
    print("licensed profiles: %s" % profiles)
    return profiles[0]


def cleanup(quiet=False):
    for path in ("/projects/%s/" % PROJECT, "/admin/users/" + USER,
                 "/admin/connections/" + CONN, "/admin/groups/" + GROUP):
        st, _ = api("DELETE", path)
        if not quiet:
            print("  cleanup %-32s http=%s" % (path, st))
    # DSS attaches a per-user group and leaves it behind when the user goes.
    st, groups = api("GET", "/admin/groups/")
    st2, users = api("GET", "/admin/users/")
    if isinstance(groups, list) and isinstance(users, list):
        logins = [u["login"] for u in users]
        for g in groups:
            n = g["name"]
            if "-admin-" in n and n.split("-admin-")[0] not in logins:
                api("DELETE", "/admin/groups/" + n)
                if not quiet:
                    print("  cleanup orphan group %s" % n)


def main():
    profile = licensed_profile()

    print("\n--- creating objects to import ---")
    cleanup(quiet=True)
    checks = [
        ("project", api("POST", "/projects/", {
            "projectKey": PROJECT, "name": "import smoke", "owner": "admin"})),
        ("variables", api("PUT", "/projects/%s/variables" % PROJECT, {
            "standard": {"env": "smoke"}, "local": {}})),
        ("group", api("POST", "/admin/groups/", {
            "name": GROUP, "description": "import smoke", "admin": False,
            "sourceType": "LOCAL"})),
        ("user", api("POST", "/admin/users/", {
            "login": USER, "displayName": "Smoke User", "sourceType": "LOCAL",
            "groups": [GROUP], "userProfile": profile,
            "password": "An0ther-passw0rd!"})),
        ("connection", api("POST", "/admin/connections/", {
            "name": CONN, "type": "Filesystem",
            "params": {"root": "/tmp/" + CONN}, "usableBy": "ALL"})),
    ]
    for label, (st, body) in checks:
        msg = body.get("message", "") if isinstance(body, dict) else ""
        print("  %-12s http=%s %s" % (label, st, msg[:70]))
        if st not in (200, 201):
            cleanup(quiet=True)
            raise SystemExit("could not create %s" % label)

    # DSS attaches its own per-user group, which the configuration has to
    # name or the first plan would want to remove that membership. This is
    # documented behaviour, not drift, so the test adopts it.
    st, u = api("GET", "/admin/users/" + USER)
    groups = u.get("groups", [GROUP]) if isinstance(u, dict) else [GROUP]
    print("  user's full membership: %s" % groups)

    os.makedirs(WORK, exist_ok=True)
    with open(os.path.join(WORK, "dev.tfrc"), "w") as f:
        f.write('provider_installation {\n  dev_overrides {\n'
                '    "amrutp24/dataiku" = "%s"\n  }\n  direct {}\n}\n' % REPO)

    with open(os.path.join(WORK, "main.tf"), "w") as f:
        f.write('''terraform {
  required_providers {
    dataiku = {
      source = "amrutp24/dataiku"
    }
  }
}

provider "dataiku" {}

resource "dataiku_project" "smoke" {
  project_key = %(project)s
  name        = "import smoke"
  owner       = "admin"
  tags        = []
}

resource "dataiku_project_variables" "smoke" {
  project_key = %(project)s
  standard    = jsonencode({ env = "smoke" })
}

resource "dataiku_group" "smoke" {
  name        = %(group)s
  description = "import smoke"
}

resource "dataiku_user" "smoke" {
  login        = %(user)s
  display_name = "Smoke User"
  user_profile = %(profile)s
  groups       = %(groups)s
}

resource "dataiku_connection" "smoke" {
  name      = %(conn)s
  type      = "Filesystem"
  usable_by = "ALL"
}
''' % {
            "project": json.dumps(PROJECT),
            "group": json.dumps(GROUP),
            "user": json.dumps(USER),
            "conn": json.dumps(CONN),
            "profile": json.dumps(profile),
            "groups": json.dumps(groups),
        })

    for stale in ("terraform.tfstate", "terraform.tfstate.backup"):
        f = os.path.join(WORK, stale)
        if os.path.exists(f):
            os.remove(f)

    print("\n--- terraform import ---")
    imports = [
        ("dataiku_project.smoke", PROJECT),
        ("dataiku_project_variables.smoke", PROJECT),
        ("dataiku_group.smoke", GROUP),
        ("dataiku_user.smoke", USER),
        ("dataiku_connection.smoke", CONN),
    ]
    failed = []
    for addr, ident in imports:
        p = tf("import", "-no-color", addr, ident, check=False)
        ok = "Import successful" in (p.stdout + p.stderr)
        print("  %-36s <- %-14s %s" % (addr, ident, "OK" if ok else "FAILED"))
        if not ok:
            failed.append(addr)
            for line in (p.stdout + p.stderr).splitlines():
                if "Error" in line:
                    print("      " + line.strip())

    print("\n--- plan after import (empty is the assertion) ---")
    p = tf("plan", "-no-color", "-detailed-exitcode", check=False)
    for line in p.stdout.splitlines():
        if (line.startswith("Plan:") or line.startswith("No changes")
                or line.strip().startswith("# dataiku")
                or line.strip().startswith("~ ") or line.strip().startswith("+ ")
                or line.strip().startswith("- ")):
            print("  " + line.strip())

    print("\n--- cleanup ---")
    cleanup()

    if failed:
        raise SystemExit("import failed for: %s" % ", ".join(failed))
    if p.returncode == 1:
        raise SystemExit("plan errored")
    if p.returncode == 2:
        raise SystemExit("FAIL: the plan after an untouched import was not empty")
    print("\nPASS: every resource imported and the plan afterwards was empty")


if __name__ == "__main__":
    main()
