#!/usr/bin/env python3
"""Validate every example under examples/ against the provider schema.

Nothing checked these before, and they are what people copy. The permission
example shipped `run_scenario` for months; the attribute is `run_scenarios`,
so anyone who pasted it got an error and had to go and find the real name.

Two kinds of example live under examples/:

  Root configurations (examples/platform, examples/provisioning/*) declare
  their own required_providers and are validated as they are.

  tfplugindocs fragments (examples/resources/*, examples/data-sources/*) are
  snippets meant to be embedded in a docs page. They have no terraform block,
  and they refer to resources they do not declare, because a realistic example
  shows things wired together. Each one is validated on its own with a
  generated provider block, and any reference to a resource it does not
  declare is replaced by a literal string first. That keeps every attribute
  name in the snippet under test, which is the part that goes wrong, without
  needing the neighbouring examples present.

Run with the provider under test already installed, or let terraform init
fetch the published one.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
EXAMPLES = os.path.join(REPO, "examples")

DECL = re.compile(r'^(resource|data)\s+"([a-z0-9_]+)"\s+"([a-z0-9_]+)"', re.M)
REF = re.compile(r'\b(data\.)?(dataiku_[a-z0-9_]+)\.([a-z0-9_]+)\.([a-z0-9_]+)\b')
VAR_DECL = re.compile(r'^variable\s+"([a-z0-9_]+)"', re.M)
VAR_REF = re.compile(r'\bvar\.([a-z0-9_]+)\b')


def provider_block(version_constraint):
    constraint = f'\n      version = "{version_constraint}"' if version_constraint else ""
    return (
        "terraform {\n"
        "  required_providers {\n"
        "    dataiku = {\n"
        '      source  = "amrutp24/dataiku"' + constraint + "\n"
        "    }\n"
        "  }\n"
        "}\n"
        "\n"
        'provider "dataiku" {\n'
        '  host    = "https://dss.example.invalid"\n'
        '  api_key = "not-used-by-validate"\n'
        "}\n\n"
    )


def declared_addresses(text):
    out = set()
    for kind, type_, name in DECL.findall(text):
        out.add(("data." if kind == "data" else "") + f"{type_}.{name}")
    return out


def delink(text):
    """Replace references to undeclared resources with literal strings.

    terraform validate stops at the first undeclared reference, which would
    hide every schema error after it. Substituting a literal keeps the
    surrounding attribute names being checked.
    """
    declared = declared_addresses(text)

    def repl(m):
        prefix, type_, name, attr = m.groups()
        addr = ("data." if prefix else "") + f"{type_}.{name}"
        if addr in declared:
            return m.group(0)
        return f'"{name}"'

    return REF.sub(repl, text)


def declare_variables(text):
    """Declare any var.* the fragment uses but does not define.

    An example that takes a password from a variable is showing the right
    habit, so the fix is to supply the declaration rather than to inline a
    secret into the example.
    """
    missing = sorted(set(VAR_REF.findall(text)) - set(VAR_DECL.findall(text)))
    return "".join(
        'variable "%s" {\n  type      = string\n  sensitive = true\n  default   = "example"\n}\n\n' % name
        for name in missing
    )


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, shell=False)


def validate_dir(path, label, failures, tf):
    init = run([tf, "init", "-backend=false", "-no-color", "-input=false"], path)
    if init.returncode != 0:
        failures.append((label, "init", init.stdout + init.stderr))
        return
    val = run([tf, "validate", "-no-color"], path)
    if val.returncode != 0:
        failures.append((label, "validate", val.stdout + val.stderr))
        print(f"  FAIL  {label}")
    else:
        print(f"  ok    {label}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", default="", help='version constraint for generated provider blocks, e.g. "~> 0.6"')
    ap.add_argument("--terraform", default="terraform")
    args = ap.parse_args()

    tf = shutil.which(args.terraform) or args.terraform
    failures = []
    work = tempfile.mkdtemp(prefix="validate-examples-")

    roots, fragments = [], []
    for dirpath, dirs, files in os.walk(EXAMPLES):
        # Never descend into a module cache or a provider cache. Those hold
        # other people's configurations, downloaded by an earlier init, and
        # they are not ours to validate.
        dirs[:] = [d for d in dirs if d != ".terraform"]
        tfs = sorted(f for f in files if f.endswith(".tf"))
        if not tfs:
            continue
        rel = os.path.relpath(dirpath, EXAMPLES).replace(os.sep, "/")
        text = "\n".join(
            open(os.path.join(dirpath, f), encoding="utf-8").read() for f in tfs
        )
        (roots if "required_providers" in text else fragments).append((rel, dirpath, tfs, text))

    print(f"root configurations ({len(roots)}):")
    for rel, dirpath, _tfs, _text in sorted(roots):
        validate_dir(dirpath, rel, failures, tf)

    print(f"\ndocs fragments ({len(fragments)}):")
    for rel, _dirpath, _tfs, text in sorted(fragments):
        target = os.path.join(work, rel.replace("/", "__"))
        os.makedirs(target, exist_ok=True)
        with open(os.path.join(target, "main.tf"), "w", encoding="utf-8", newline="\n") as fh:
            body = delink(text)
            fh.write(provider_block(args.version) + declare_variables(body) + body)
        validate_dir(target, rel, failures, tf)

    shutil.rmtree(work, ignore_errors=True)

    if failures:
        print(f"\n{len(failures)} example(s) failed:\n")
        for label, stage, output in failures:
            print(f"--- {label} ({stage})")
            print(output.strip()[:1500])
            print()
        return 1
    print(f"\nall {len(roots) + len(fragments)} examples validate")
    return 0


if __name__ == "__main__":
    sys.exit(main())
