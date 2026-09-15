#!/usr/bin/env bash
#
# Six associations for the covid court on gnoland-1.
#
#   bash scratchpad/add-relations.sh
#
# IT ASKS FOR THE ~/.kourtseed PASSWORD ONCE and reuses it for all six, rather
# than prompting per transaction. Nothing is echoed and nothing is written down.
#
# WHY THIS KEY. kourtseed is g1pm5kng23…, which AUTHORED ALL 24 COVID CLAIMS —
# and AddAssociation waives the bond for the author of the FROM claim:
#
#     if f.author != who && !isActiveMod(ensureMod(c), who) { bond = assocBondFor(c) }
#
# So these six cost gas and nothing else. Signing as the admin instead would post
# 1,000,000 CC per edge (6,000,000 total, refundable) because the admin authored
# none of them and IsCourtMod(covid, admin) is false — and that key is a Ledger,
# so it would also want six physical confirmations.
#
# BALANCE, CHECKED: kourtseed holds 425,057,200ugnot. Six transactions at a 1
# GNOT fee is 6 of 425. The fee is deducted IN FULL whether the gas is used or
# not, and the floor is gas-wanted/1000.
#
# STATE, CHECKED: covid holds 24 claims and every one reports `out:;in:` — there
# are no associations at all yet. Caps are far off (32 out, 64 in, 4 per author);
# the heaviest target below is #12, with two inbound.
set -euo pipefail

REALM=gno.land/r/g1ecsuj0q572jr0dhu29q9njtnmw03hyu7tyyvv6/kourt
KEY=kourtseed
HOMEDIR="$HOME/.kourtseed"
REMOTE=https://rpc.gno.land:443
CHAINID=gnoland-1

command -v gnokey >/dev/null || { echo "gnokey not on PATH" >&2; exit 2; }
gnokey list -home "$HOMEDIR" 2>/dev/null | grep -q "$KEY" || {
  echo "no '$KEY' in $HOMEDIR" >&2; exit 2; }

# -s: not echoed. Read once, held in a shell variable for the life of the script
# and never written to a file or a command line — a password in argv is visible
# in `ps` to every process on the machine.
printf 'passphrase for %s: ' "$HOMEDIR" >&2
read -r -s PASS
echo >&2

add() { # from to stance why
  echo
  echo "==> #$1 $3 #$2  — $4"
  gnokey maketx call \
    -pkgpath "$REALM" -func AddAssociation \
    -args covid -args "$1" -args "$2" -args "$3" \
    -gas-wanted 10000000 -gas-fee 1000000ugnot \
    -broadcast -chainid "$CHAINID" -remote "$REMOTE" \
    -home "$HOMEDIR" -insecure-password-stdin "$KEY" <<EOF
$PASS
EOF
}

# ---- the miscarriage cluster -------------------------------------------------
add 6 5 contests \
  "#6 names the denominator that produces #5's 82%: losses over completed pregnancies only"
add 23 8 contests \
  "#23's ten thousand vaccinated pregnancies with no adverse signal run against #8's raised risk"

# ---- the origin cluster ------------------------------------------------------
add 3 2 contests \
  "#3 traces every published version of #2 to preprints no peer review accepted"
add 9 12 supports \
  "#9's DEFUSE proposal describes the insertion #12's conclusion rests on"
add 1 12 supports \
  "#1's furin site with no close analogue points the same way as #12"
add 4 20 supports \
  "#4's federal grants are the funding #20's P3CO finding is about"

echo
echo "==> done — ask Claude to verify on chain and check the map draws them."
