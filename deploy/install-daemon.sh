#!/bin/sh
# Installs hahod as a launchd user agent: builds the binary, renders the
# plist template with absolute paths, places it in ~/Library/LaunchAgents,
# and bootstraps it. Run once (or again after a relocation, or after the
# agent CLI moves); from then on the daemon is managed with launchctl like
# any other agent:
#
#   launchctl bootout   gui/$UID/com.janearc.hahod     # stop (graceful)
#   launchctl bootstrap gui/$UID ~/Library/LaunchAgents/com.janearc.hahod.plist
#   launchctl kickstart -k gui/$UID/com.janearc.hahod  # restart
#   launchctl print     gui/$UID/com.janearc.hahod     # status
#
# No secret values pass through here, ever.
set -eu

repo="$(cd "$(dirname "$0")/.." && pwd)"
label="com.janearc.hahod"
plist="$HOME/Library/LaunchAgents/$label.plist"
logdir="$HOME/.local/state/haho/log"

# The agent CLI the CLAUDE_CLI backend execs. launchd starts hahod with
# PATH=/usr/bin:/bin:/usr/sbin:/sbin and nothing else, so a bare `claude`
# resolves in the operator's shell and in NO launchd agent -- every
# CLAUDE_CLI job would land FAILED with exec-not-found while /health stayed
# green. The absolute path is stamped into the plist at install time, the
# same way @BIN@ and @LOG@ are, resolved from the installing shell (which
# does carry the nvm bin). HAHOD_CLAUDE_CLI in the installing environment
# overrides the lookup; a missing CLI refuses the install rather than
# rendering a daemon that cannot run the jobs it exists for.
claude_cli="${HAHOD_CLAUDE_CLI:-$(command -v claude || true)}"
if [ -z "$claude_cli" ]; then
  echo "install-daemon: no agent CLI found: put claude on PATH or set HAHOD_CLAUDE_CLI" >&2
  exit 1
fi
case "$claude_cli" in
  /*) ;;
  *) echo "install-daemon: HAHOD_CLAUDE_CLI must be an absolute path, got '$claude_cli'" >&2; exit 1 ;;
esac
if [ ! -x "$claude_cli" ]; then
  echo "install-daemon: agent CLI '$claude_cli' is not executable" >&2
  exit 1
fi

cd "$repo"
go build -o bin/hahod ./cmd/hahod

mkdir -p "$logdir" "$HOME/Library/LaunchAgents"
sed -e "s|@BIN@|$repo/bin/hahod|" -e "s|@LOG@|$logdir/hahod.log|" \
    -e "s|@CLAUDE@|$claude_cli|" \
    "deploy/$label.plist.in" > "$plist"

launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"
echo "hahod installed and started: $label"
echo "  agent cli: $claude_cli"
echo "  stop:    launchctl bootout gui/\$UID/$label"
echo "  start:   launchctl bootstrap gui/\$UID $plist"
echo "  restart: launchctl kickstart -k gui/\$UID/$label"
echo "  log:     $logdir/hahod.log"
