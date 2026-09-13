#!/usr/bin/env bash
# RUN THIS ON THE AIR-GAPPED MACHINE.
#
# It uses `minsign`, not gnokey. minsign imports no network package at all --
# no net/http, no crypto/tls, no crypto/x509 -- which buys two things:
#
#   1. It CANNOT phone home. Not "is configured not to": the code to open a
#      socket is not in the binary.
#   2. It runs on old macOS. gnokey's crypto/x509 binds SecTrustCopyCertificate-
#      Chain, a macOS 12 symbol resolved at load, so gnokey refuses to launch on
#      anything older even though signing never touches the network.
#
# minsign's output was verified byte-for-byte identical to real gnokey's across
# all six packages, same key and same inputs. See MANIFEST.txt to repeat that
# check yourself on any machine where gnokey does run.
#
# WHY THE SEQUENCES ARE POSITIONAL. An offline machine cannot ask the chain what
# an account's sequence is, and the signature covers it -- so the six below are
# ACCNUM's sequence counting up from 0, which assumes that address has sent
# NOTHING on this chain before the batch. Read ACCNUM and confirm the sequence
# with scripts/mainnet-actors.py just before signing; if any transaction from
# the address lands first, every signature here is void. A wrong sequence costs
# a rejected broadcast, not money.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
NS="${NS:?set NS to the deploying address, e.g. NS=g1ecsuj...}"
CHAINID=gnoland-1
ACCNUM="${ACCNUM:?set ACCNUM from scripts/mainnet-actors.py}"
EXPECT_ADDR="${EXPECT_ADDR:-$NS}"

pick() {
  [ -n "${MINSIGN:-}" ] && { echo "$MINSIGN"; return; }
  local os arch ext=""
  case "$(uname -s)" in
    Darwin) os=darwin ;; Linux) os=linux ;; FreeBSD) os=freebsd ;;
    OpenBSD) os=openbsd ;; NetBSD) os=netbsd ;;
    CYGWIN*|MINGW*|MSYS*) os=windows; ext=".exe" ;; *) os=unknown ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) arch=unknown ;;
  esac
  echo "$HERE/minsign-builds/minsign-$os-$arch$ext"
}
MINSIGN="$(pick)"

if [ ! -x "$MINSIGN" ]; then
  echo "No minsign for this machine at:"
  echo "  $MINSIGN"
  echo
  echo "In the bundle:"
  ls "$HERE/minsign-builds" 2>/dev/null || echo "  (none)"
  echo
  echo "Set MINSIGN=/path/to/it, or build from the source in minsign-src/."
  exit 1
fi
echo "Using $(basename "$MINSIGN")"

# n<TAB>dir<TAB>pkgpath<TAB>gas-wanted<TAB>gas-fee
# Gas measured, not guessed: a real deploy of p/checkpoint (15,793 bytes) burned
# 26,952,059 gas, ~1,707 gas/byte; these carry ~2x on that rate. The fee must be
# at least gas-wanted/1000 (the chain enforces a block gas price even though
# auth:p:min_gas_price reads empty) and is deducted IN FULL regardless of use.
PKGS="\
1	p/checkpoint	gno.land/p/$NS/checkpoint/v0	40000000	1000000
2	p/curve	gno.land/p/$NS/curve/v0	40000000	1000000
3	p/twap	gno.land/p/$NS/twap/v0	50000000	1000000
4	p/grc20votes	gno.land/p/$NS/grc20votes/v0	90000000	1000000
5	p/governor	gno.land/p/$NS/governor/v0	450000000	1000000
6	r/kourt	gno.land/r/$NS/kourt	900000000	1125000"

# The mnemonic is read once, here, and piped to each call. It is never an
# argument, so it cannot appear in ps output or a shell history; minsign holds
# it in memory and writes only the signed transaction.
printf 'BIP39 mnemonic (input hidden): ' >&2
read -rs MNEMONIC; echo >&2
echo

while IFS=$'\t' read -r n dir path gas fee; do
  seq=$((n - 1))
  out="$HERE/signed-$n-$(basename "$dir").tx"
  printf '[%s/6] %-14s seq=%s\n' "$n" "$dir" "$seq"

  printf '%s\n' "$MNEMONIC" | "$MINSIGN" \
    "$HERE/$dir" "$path" "$gas" "$fee" 200000000 "$CHAINID" "$ACCNUM:$seq" \
    > "$out" 2> "$out.log"

  # THE ADDRESS CHECK IS THE IMPORTANT ONE. A mnemonic typo, or a different
  # derivation path, yields a perfectly valid signature from an address that
  # holds nothing -- which fails only at broadcast, after the USB round trip.
  #
  # Anchored on the "signer" line, NOT on the whole log. An earlier version
  # grepped the log for the address and passed with a deliberately wrong
  # mnemonic every time: the package PATH contains the same address, so the
  # match never had anything to do with who signed.
  if ! grep -q "^  signer  $EXPECT_ADDR\$" "$out.log"; then
    echo
    echo "WRONG SIGNER. Expected $EXPECT_ADDR"
    cat "$out.log"
    rm -f "$HERE"/signed-*.tx "$HERE"/signed-*.tx.log
    exit 1
  fi
  # An unsigned document ends "signatures":null and contains no "signature":".
  if ! grep -q '"signature":"' "$out"; then
    echo "NOT SIGNED: $out" >&2
    exit 1
  fi
  rm -f "$out.log"
  echo "      -> $(basename "$out") ($(wc -c < "$out" | tr -d ' ') bytes)"
done <<< "$PKGS"

echo
echo "Done. Copy ONLY the signed-*.tx files back to the networked machine."
