#!/usr/bin/env bash
#
# "Origins" and "Proximal Origin" stop being two headings that read as one.
#
#   bash scratchpad/dedupe-origin-sets.sh
#
# IT ASKS FOR THE ~/.kourtseed PASSWORD ONCE and reuses it for all four, the
# same way add-relations.sh does. Nothing is echoed and nothing is written down.
#
# WHAT WAS ACTUALLY WRONG, read off the chain before any of this was written:
#
#   folder 1  "Origins"          bornOf=0  claims 1,2,3,4,9,10,12,13
#   folder 5  "Proximal Origin"  bornOf=0  claims 7,11,21
#
# The two are NOT duplicates. Folder 1 is about the virus — the furin site (#1),
# the bioweapon claim and where it came from (#2, #3), the DEFUSE proposal (#9),
# Baric (#10), the lab-leak conclusion (#12), the money at WIV (#4). Folder 5 is
# about the PAPER and the statement beside it: what its authors privately
# thought (#11), who prompted the draft (#21), and the Lancet organiser's
# concealed funding (#7). Different questions, one of which is about the world
# and the other about the people who described it.
#
# What made them read as redundant is the NAME. Two top-level headings, both
# opening with "Origin", is a collision a reader resolves as duplication before
# they have read a single claim under either.
#
# SO THE FIX IS THE LABEL AND THE PLACE, NOT A MERGE. Merging would throw away a
# distinction this court will want back the moment somebody files another claim
# about how a paper was written. Four acts:
#
#   1. rename 5, so its name says which two documents it is about
#   2. nest 5 under 1, so they stop competing at the top level
#   3. file #13 into 5 — "Baric was excluded from the Proximal Origin author
#      list" is a claim about the paper, and it has been sitting in Origins
#   4. take #13 out of 1, in that order, so it is never unfiled in between
#
# BORN OF NOTHING, WHICH IS WHY NO VOTE IS INVOLVED. SetBornOf reports 0 for
# both folders: a moderator created them, the court never voted them into
# existence, so there is no verdict binding either name. A governed set — one
# carried by a 𓂀 claim — would need the court, and retiring one never frees its
# claim.
#
# WHY THIS KEY. requireFolderMod wants an active moderator of the court, and
# covid runs 1-of-1: g1pm5kng23…, which is this key. CHECKED on chain —
# IsCourtMod("covid", g1pm5kng23…) is true, and it is false for every key in
# ~/.gnokey, including the Ledger that owns the realm. The admin key cannot do
# this at all; this one can, and single-signer means one transaction per act
# with nothing to co-sign.
#
# CHECKED BEFORE WRITING: folder 5 is neither retired nor purged, both descs are
# empty so the rename overwrites nothing, folder 1 sits at the root and
# mustNestable allows depth 1 + height 1 against a limit of 4, folder 5 holds 3
# items against a cap of 200, and kourtseed holds 377,983,200ugnot — four
# transactions at a 1 GNOT fee is 4 of 377. The fee is deducted in full whether
# the gas is used or not.
#
# NOT IDEMPOTENT, ON PURPOSE. Run it twice and step 4 panics with "that claim is
# not in the folder", because by then it is true. set -e stops there, and every
# act before it has already happened exactly once.
#
# 50M GAS, NOT THE 10M add-relations.sh USES, AND MEASURED RATHER THAN GUESSED.
# The first run of this script died on the rename: "gas used (23580003) exceeds
# tx's gas wanted (10000000)". An association edge is a small write; a rename
# carrying a 176-character description through this realm's state is not, and
# the two are not interchangeable numbers. Nothing landed — empty tx hash,
# height 0, and FolderName read back unchanged — because the ceiling is checked
# during simulation, before the transaction is broadcast.
# THE FEE DOES NOT MOVE WITH IT. gas-fee is what is actually deducted, in full,
# used or not; gas-wanted is only the ceiling, and its floor here is
# gas-wanted/1000 = 50,000ugnot against the 1,000,000ugnot below. So the
# headroom costs nothing and running out costs the whole fee for nothing.
set -euo pipefail

REALM=gno.land/r/g1ecsuj0q572jr0dhu29q9njtnmw03hyu7tyyvv6/kourt
KEY=kourtseed
HOMEDIR="$HOME/.kourtseed"
REMOTE=https://rpc.gno.land:443
CHAINID=gnoland-1

command -v gnokey >/dev/null || { echo "gnokey not on PATH" >&2; exit 2; }
gnokey list -home "$HOMEDIR" 2>/dev/null | grep -q "$KEY" || {
  echo "no '$KEY' in $HOMEDIR" >&2; exit 2; }

# THE PASSPHRASE COMES FROM THE 0600 FILE BESIDE THE KEYBASE, and falls back to
# asking. Held in a shell variable for the life of the script, never echoed,
# never written anywhere, and never passed in argv — a password on a command
# line is visible in `ps` to every process on the machine. The heredoc below is
# the only thing that ever sees it, which is why this script must never be run
# under `set -x`.
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

# ---- 1. the name, which is the whole of what a reader experiences ------------
# It names the two documents rather than the subject, so it cannot be read as a
# second heading about the virus. 39 characters against a limit of 200.
call RenameFolder "name the documents, not the subject" \
  covid 5 \
  "Proximal Origin and the Lancet statement" \
  "How the two public statements on the origin question were produced: who drafted them, who was left off the author list, and what their authors privately believed while writing."

# ---- 2. the place -----------------------------------------------------------
# Under Origins rather than beside it. The claims about how the question was
# answered publicly belong with the question, one level down.
call MoveFolder "nest it under Origins instead of beside it" \
  covid 5 1

# ---- 3 & 4. the stray, added before it is removed ----------------------------
# #13 is "Baric was excluded from the Proximal Origin author list on the ground
# that he was too close to the WIV" — a claim about the paper's authorship that
# has been filed under the virus's origin. A claim may sit in several folders
# (claim 4 is in both Origins and Gain-of-function funding), so adding first
# leaves it filed throughout.
call AddToFolder "#13 is about the paper's author list" \
  covid 5 13
call RemoveFromFolder "...so it comes out of Origins" \
  covid 1 13

echo
echo "==> done — ask Claude to verify on chain and check the court page draws it."
