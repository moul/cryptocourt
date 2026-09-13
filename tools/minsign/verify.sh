#!/usr/bin/env bash
# Prove a minsign binary signs exactly what stock gnokey signs.
#
#   GNOKEY=/path/to/gnokey ./tools/minsign/verify.sh /path/to/minsign [pkgdir]
#
# THIS IS THE ONLY REASON TO TRUST minsign. build.sh makes three patches to gno's
# own code — two of them to reach a Go old enough to target macOS 10.13, one of
# them to the ECDSA call site itself. None of that is safe because the diff looks
# small. It is safe because these signatures are RFC6979-deterministic: the same
# key over the same message must produce the same bytes, so if minsign and gnokey
# agree byte for byte, the cryptography is the same cryptography.
#
# A throwaway BIP39 phrase is used — the all-`abandon` vector. No real key is
# needed to compare two signers, and none should be used: this script has no
# business touching one.
set -eu

MS="${1:?usage: verify.sh <minsign-binary> [pkgdir]}"
PKGDIR="${2:-}"
GNOKEY="${GNOKEY:-gnokey}"
command -v "$GNOKEY" >/dev/null 2>&1 || [ -x "$GNOKEY" ] || {
  echo "no gnokey (set GNOKEY=/path/to/it)" >&2; exit 1; }
[ -x "$MS" ] || { echo "no minsign at $MS" >&2; exit 1; }

# The published all-`abandon` test vector. Deliberately the most widely known
# phrase there is, so nobody mistakes it for a key worth protecting.
M="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
CHAIN=gnoland-1
ACCT=3007863
SEQ=3

H="$(mktemp -d -t minsign-verify)"
trap 'rm -rf "$H"' EXIT INT TERM
printf '%s\n\n\n' "$M" | "$GNOKEY" -home "$H" add -recover \
  -insecure-password-stdin vkey >/dev/null 2>&1

fail=0

if [ -n "$PKGDIR" ]; then
  printf '\n' | "$GNOKEY" -home "$H" maketx addpkg -pkgdir "$PKGDIR" \
    -pkgpath gno.land/p/verify/x/v0 -gas-fee 1125000ugnot -gas-wanted 900000000 \
    -max-deposit 200000000ugnot -broadcast=false -insecure-password-stdin vkey \
    > "$H/gk.tx" 2>/dev/null
  printf '\n' | "$GNOKEY" -home "$H" sign -tx-path "$H/gk.tx" -chainid "$CHAIN" \
    -account-number "$ACCT" -account-sequence "$SEQ" \
    -insecure-password-stdin vkey >/dev/null 2>&1
  printf '%s\n' "$M" | "$MS" "$PKGDIR" gno.land/p/verify/x/v0 \
    900000000 1125000 200000000 "$CHAIN" "$ACCT:$SEQ" > "$H/ms.tx" 2>/dev/null
  if cmp -s "$H/gk.tx" "$H/ms.tx"; then
    printf '  addpkg  IDENTICAL  %s bytes\n' "$(wc -c < "$H/gk.tx" | tr -d ' ')"
  else
    printf '  addpkg  DIFFER  gnokey=%s minsign=%s\n' \
      "$(wc -c < "$H/gk.tx" | tr -d ' ')" "$(wc -c < "$H/ms.tx" | tr -d ' ')"
    fail=1
  fi
fi

# m_call, with an argument that is easy to mangle and hard to notice: an
# astral-plane character, an apostrophe and a quote. The seed's own claim titles
# carry U+13080, so this is the real shape, not a contrived one.
printf '[{"who":"vkey","func":"OpenClaimP","args":["covid","\\ud80c\\udc80 O'"'"'Brien \\"x\\"","body"],"send":""}]\n' \
  > "$H/plan.json"
printf '{"vkey":{"index":0,"account_number":%s,"sequence":%s}}\n' "$ACCT" "$SEQ" > "$H/actors.json"
mkdir -p "$H/out"
printf '%s\n' "$M" | "$MS" plan "$H/plan.json" "$H/actors.json" \
  gno.land/r/verify/kourt 90000000 1000000 "$CHAIN" "$H/out" >/dev/null 2>&1

ARGS=$(python3 - "$H/plan.json" <<'PY'
import json, shlex, sys
row = json.load(open(sys.argv[1]))[0]
print(" ".join("-args " + shlex.quote(a) for a in row["args"]))
PY
)
eval printf "'\n'" \| '"$GNOKEY"' -home '"$H"' maketx call -pkgpath gno.land/r/verify/kourt \
  -func OpenClaimP "$ARGS" -gas-fee 1000000ugnot -gas-wanted 90000000 \
  -broadcast=false -insecure-password-stdin vkey > "$H/gkc.tx" 2>/dev/null
printf '\n' | "$GNOKEY" -home "$H" sign -tx-path "$H/gkc.tx" -chainid "$CHAIN" \
  -account-number "$ACCT" -account-sequence "$SEQ" \
  -insecure-password-stdin vkey >/dev/null 2>&1

MSC=$(ls "$H/out"/call-*.tx 2>/dev/null | head -1)
if [ -n "$MSC" ] && cmp -s "$H/gkc.tx" "$MSC"; then
  printf '  m_call  IDENTICAL  %s bytes (astral-plane argument intact)\n' \
    "$(wc -c < "$H/gkc.tx" | tr -d ' ')"
else
  printf '  m_call  DIFFER\n'
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "minsign does NOT sign what gnokey signs. Do not use this binary." >&2
  exit 1
fi
echo
echo "minsign signs exactly what gnokey signs."
