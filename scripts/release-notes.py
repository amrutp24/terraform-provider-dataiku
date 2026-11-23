#!/usr/bin/env python3
"""Print one version's section of CHANGELOG.md, for use as release notes.

    python3 scripts/release-notes.py 0.8.1

The release workflow runs this before it builds anything. Every release up to
0.8.1 went out with an empty page on GitHub, because goreleaser's own
changelog is turned off and nothing took its place. The text existed the
whole time, in CHANGELOG.md.

Exits non-zero when the version has no section or the section is empty, so a
tag pushed before the changelog was stamped fails early and loudly instead of
publishing nothing.
"""
import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def section(changelog, version):
    """The body under '## [version] ...', up to the next '## [' heading."""
    heading = re.compile(r"^## \[%s\][^\n]*\n" % re.escape(version), re.M)
    found = heading.search(changelog)
    if not found:
        return None
    rest = changelog[found.end():]
    following = re.search(r"^## \[", rest, re.M)
    return (rest[:following.start()] if following else rest).strip()


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: release-notes.py VERSION   (for example 0.8.1, without the v)")
    version = sys.argv[1].lstrip("v")

    with open(os.path.join(REPO, "CHANGELOG.md"), encoding="utf-8") as f:
        changelog = f.read().replace("\r\n", "\n")

    body = section(changelog, version)
    if body is None:
        hint = ""
        if section(changelog, "Unreleased"):
            hint = (" There is an [Unreleased] section: rename it to [%s] with today's date,"
                    " merge that, then tag." % version)
        raise SystemExit("CHANGELOG.md has no section for %s.%s" % (version, hint))
    if not body:
        raise SystemExit("The CHANGELOG.md section for %s is empty." % version)

    # Bytes, so the output has the same line endings on every platform.
    sys.stdout.buffer.write((body + "\n").encode("utf-8"))


if __name__ == "__main__":
    main()
