#!/usr/bin/env bash
#
# The Baric author-list claim, moved on the copy a reader can actually see.
#
#   bash scratchpad/refile-baric-claim.sh
#
# WHAT WENT WRONG IN dedupe-origin-sets.sh, which this repairs. That script
# moved claim #13 — "Ralph Baric was excluded from the Proximal Origin author
# list" — out of Origins and into the renamed folder. The move worked and
# changed nothing a reader sees, because #13 is HIDDEN.
#
# THE COURT HOLDS EVERY CLAIM TWICE. It was re-seeded: the mod log carries a run
# of hides at blocks 49775-49785 reading "re-seeded: body carried a literal
# backslash-n and the verdict was posted without reading the claim", and the old
# rows stayed in their folders. So folder 1 holds 1,2,3,4,9,10,12 (hidden) and
# 25,26,27,28,33,34,36 (live), which are the same seven claims twice, and #37 is
# the live twin of #13. MEASURED, not inferred: HiddenFromListing is true for
# every old id and false for every new one.
#
# WHY THE FIRST SCRIPT COULD NOT HAVE KNOWN. It read the folder at 15:4x and the
# re-seed landed while it was being written — folder 1 held eight items then and
# fifteen by the time the transactions were verified. The lesson is not "read
# faster", it is that an id in this court is not a claim: ask
# HiddenFromListing before acting on one.
#
# THE SHADOW ROW IS LEFT WHERE IT IS, and that is deliberate rather than lazy:
# #13 now sits in folder 5 and #37 will too, so the hidden copy is filed exactly
# like the visible one. Undoing it would put the two copies of one claim under
# two different headings, which is worse than harmless.
#
# 50M GAS, for the reason dedupe-origin-sets.sh records: a write against this
# realm's state costs ~24M, and the 10M in add-relations.sh is an association
# edge, not a folder act.
set -euo pipefail

REALM=gno.land/r/g1ecsuj0q572jr0dhu29q9njtnmw03hyu7tyyvv6/kourt
KEY=kourtseed
HOMEDIR="$HOME/.kourtseed"
REMOTE=https://rpc.gno.land:443
CHAINID=gnoland-1

command -v gnokey >/dev/null || { echo "gnokey not on PATH" >&2; exit 2; }

PWFILE="$HOMEDIR/.pw"
if [ -r "$PWFILE" ]; then
  PASS=$(cat "$PWFILE")
else
  printf 'passphrase for %s: ' "$HOMEDIR" >&2
  read -r -s PASS
  echo >&2
fi

call() { # func why arg...
  local fn="$1" why="$2"; shift 2
  local args=()
  for a in "$@"; do args+=(-args "$a"); done
  echo
  echo "==> $fn  — $why"
  gnokey maketx call \
    -pkgpath "$REALM" -func "$fn" \
    "${args[@]}" \
    -gas-wanted 50000000 -gas-fee 1000000ugnot \
    -broadcast -chainid "$CHAINID" -remote "$REMOTE" \
    -home "$HOMEDIR" -insecure-password-stdin "$KEY" <<EOF
$PASS
EOF
}

# Added before it is removed, so the claim is never unfiled.
call AddToFolder "#37 is the live Baric author-list claim" \
  covid 5 37
call RemoveFromFolder "...so it comes out of Origins" \
  covid 1 37

echo
echo "==> done"
