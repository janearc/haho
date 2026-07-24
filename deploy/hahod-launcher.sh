#!/bin/sh
# hahod-launcher -- the launchd entry point: resolve secrets from 1Password
# into the daemon's environment, then become hahod. This is the delightd
# wrapper's credential pattern applied to a launchd agent: the value exists
# only in this process's environment on its way into exec -- never in a
# file, never in argv, never in a log.
#
# Each secret is one env var resolved from one op:// item reference. The
# reference (not the value) is overridable by environment; the default is
# the canonical item. The operator creates the item and puts the value in
# the vault; nothing here ever writes one.
#
# Fail-soft by design: if `op read` cannot answer (1Password app not
# running, integration off, item absent), hahod starts WITHOUT the var and
# admission refuses any JobSpec naming it -- secret_env_missing, loud and
# specific, at task time. A keyless hahod that says so beats no hahod. The
# known 1Password failure mode (app self-updated, CLI socket gone; see
# delightd docs/recovery.md) fails fast here, not hung.
set -eu

item="${HOLDEN_ANTHROPIC_KEY_ITEM:-op://Personal/holden-anthropic-key/credential}"

if command -v op >/dev/null 2>&1; then
  if value="$(op read "$item" 2>/dev/null)" && [ -n "$value" ]; then
    export HOLDEN_ANTHROPIC_KEY="$value"
    unset value
    echo "hahod-launcher: HOLDEN_ANTHROPIC_KEY resolved from 1Password" >&2
  else
    echo "hahod-launcher: could not resolve $item; starting WITHOUT the key -- api-lane jobs naming HOLDEN_ANTHROPIC_KEY will be refused at admission" >&2
  fi
else
  echo "hahod-launcher: op not on PATH; starting WITHOUT the key -- api-lane jobs naming HOLDEN_ANTHROPIC_KEY will be refused at admission" >&2
fi

exec "@BIN@" "$@"
