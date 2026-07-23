#!/bin/sh
# Installs hahod as a launchd user agent: builds the binary, renders the
# plist template with absolute paths, places it in ~/Library/LaunchAgents,
# and bootstraps it. Run once (or again after a relocation); from then on
# the daemon is managed with launchctl like any other agent:
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

cd "$repo"
go build -o bin/hahod ./cmd/hahod

mkdir -p "$logdir" "$HOME/Library/LaunchAgents"
sed -e "s|@BIN@|$repo/bin/hahod|" -e "s|@LOG@|$logdir/hahod.log|" \
    "deploy/$label.plist.in" > "$plist"

launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"
echo "hahod installed and started: $label"
echo "  stop:    launchctl bootout gui/\$UID/$label"
echo "  start:   launchctl bootstrap gui/\$UID $plist"
echo "  restart: launchctl kickstart -k gui/\$UID/$label"
echo "  log:     $logdir/hahod.log"
