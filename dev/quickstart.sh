#!/usr/bin/env bash
# From nothing to a DSS with a Terraform-made project in it.
#
#   dev/quickstart.sh             start DSS, make an API key, apply examples/quickstart
#   dev/quickstart.sh --no-apply  stop after the key, for writing your own configuration
#
# Needs Docker with the compose plugin, curl, and terraform unless --no-apply.
# Works in bash on Linux, macOS, WSL and Git Bash. On Windows without bash,
# use quickstart.ps1, which does the same thing.
#
# Safe to run again. It reuses the instance and the key it made last time.
#
# DSS_PORT and DSS_CONTAINER_NAME change the port and the container name, for
# when 10000 is taken. To run a second instance next to the first, set
# COMPOSE_PROJECT_NAME as well so it gets a data volume of its own.
set -euo pipefail

APPLY=1
if [ "${1:-}" = "--no-apply" ]; then
  APPLY=0
elif [ -n "${1:-}" ]; then
  echo "usage: $0 [--no-apply]" >&2
  exit 2
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(dirname "$HERE")"
PORT="${DSS_PORT:-10000}"
CONTAINER="${DSS_CONTAINER_NAME:-dataiku-dss-dev}"
HOST="http://localhost:${PORT}"
ENV_FILE="$HERE/.quickstart.env"

for tool in docker curl; do
  command -v "$tool" >/dev/null || { echo "$tool is not installed or not on PATH" >&2; exit 1; }
done
if [ "$APPLY" = 1 ]; then
  command -v terraform >/dev/null || {
    echo "terraform is not installed or not on PATH. Install it, or pass --no-apply." >&2
    exit 1
  }
fi

status() {
  # The HTTP status of a call to the public API, or 000 while nothing answers.
  curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@" 2>/dev/null || true
}

echo "Starting DSS (the image is about 4 GB the first time)"
docker compose -f "$HERE/docker-compose.yml" up -d dss

# 401 is the answer to wait for: the backend is up and wants a key. Before
# that the port refuses, then nginx answers 502 while the backend starts.
echo "Waiting for DSS at $HOST. A first start takes several minutes."
waited=0
until [ "$(status "$HOST/public/api/auth/info")" = "401" ]; do
  if [ "$waited" -ge 900 ]; then
    echo "DSS did not come up in 15 minutes. See: docker logs $CONTAINER" >&2
    exit 1
  fi
  sleep 5
  waited=$((waited + 5))
  if [ $((waited % 30)) = 0 ]; then echo "  still starting (${waited}s)"; fi
done
echo "DSS is up."

# Reuse the key from an earlier run if the instance still accepts it.
KEY=""
if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  KEY="$(. "$ENV_FILE" && printf '%s' "${DATAIKU_API_KEY:-}")"
  if [ -n "$KEY" ] && [ "$(status -u "$KEY:" "$HOST/public/api/auth/info")" != "200" ]; then
    KEY=""
  fi
fi

if [ -z "$KEY" ]; then
  echo "Creating an admin API key"
  # The path is relative on purpose: Git Bash rewrites an argument that
  # starts with a slash into a Windows path.
  for attempt in 1 2 3 4 5 6; do
    KEY="$(docker exec "$CONTAINER" bash -lc \
      'dss/bin/dsscli api-key-create --admin true --label terraform-quickstart --output json' 2>/dev/null \
      | grep -o 'dkuaps-[A-Za-z0-9]*' | head -n 1 || true)"
    [ -n "$KEY" ] && break
    sleep 5
  done
  if [ -z "$KEY" ]; then
    echo "Could not create an API key. Try by hand:" >&2
    echo "  docker exec $CONTAINER bash -lc 'dss/bin/dsscli api-key-create --admin true --label terraform --output json'" >&2
    exit 1
  fi
  umask 077
  printf 'export DATAIKU_HOST=%s\nexport DATAIKU_API_KEY=%s\n' "$HOST" "$KEY" > "$ENV_FILE"
fi

export DATAIKU_HOST="$HOST"
export DATAIKU_API_KEY="$KEY"

if [ "$APPLY" = 1 ]; then
  echo "Applying examples/quickstart"
  terraform -chdir="$REPO/examples/quickstart" init -input=false >/dev/null
  terraform -chdir="$REPO/examples/quickstart" apply -input=false -auto-approve
fi

cat <<EOF

Done.

  DSS:      $HOST   (sign in as admin / admin)
  Key:      saved in dev/.quickstart.env, which git ignores

The first time you open DSS it asks you to register for the Free Edition or
enter a licence. That is DSS, and nothing here needs either.

EOF
if [ "$APPLY" = 1 ]; then
  echo "  Project:  $HOST/projects/QUICKSTART/"
  echo
fi
cat <<EOF
To use the provider yourself from this shell:

  source dev/.quickstart.env
  terraform -chdir=examples/quickstart plan

To throw everything away, data included:

  docker compose -f dev/docker-compose.yml down -v
EOF
