#!/usr/bin/env bash
# RUN THIS ON THE NETWORKED MACHINE, after 1-sign-OFFLINE.sh.
#
# Broadcasts the six signed transactions in dependency order. Holds no key and
# asks for no password -- the signatures are already in the files.
#
# STOPS AT THE FIRST FAILURE, and that matters more than usual: the signatures
# are over consecutive sequences, so if #3 fails, #4's signature (made for
# sequence 3) is for a sequence the chain has not reached. Fix #3 and continue;
# do not skip it.
#
# broadcast does NOT simulate, which is deliberate. gnokey's pre-broadcast
# simulate rewrites GasWanted and re-signs, and a client/chain version skew
# there produced "signature verification failed" for signatures the chain then
# accepted. This path never runs it.
set -eu

REMOTE="${REMOTE:-https://rpc.gno.land:443}"
HERE="$(cd "$(dirname "$0")" && pwd)"

# PICK THE BINARY FOR THIS MACHINE. The bundle ships one per platform because
# whoever built it does not know what this machine is. gno refuses to compile on
# 32-bit by design (nocompile_on_32bits.go), so there is no 386 build to find.
pick_gnokey() {
  [ -n "${GNOKEY:-}" ] && { echo "$GNOKEY"; return; }
  local os arch
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    FreeBSD) os=freebsd ;;
    OpenBSD) os=openbsd ;;
    CYGWIN*|MINGW*|MSYS*) os=windows ;;
    *) os=unknown ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) arch=unknown ;;
  esac
  local ext=""; [ "$os" = windows ] && ext=".exe"
  echo "$HERE/gnokey-builds/gnokey-$os-$arch$ext"
}
GNOKEY="$(pick_gnokey)"

# No package list, gas or chain-id here on purpose: all of that is already
# inside the signed files, and a second copy of it in this script would be a
# second thing to keep in step with the signing side.
if [ ! -x "$GNOKEY" ]; then
  echo "No gnokey for this machine at:"
  echo "  $GNOKEY"
  echo
  echo "Available in the bundle:"
  ls "$HERE/gnokey-builds" 2>/dev/null || echo "  (none)"
  echo
  echo "Set GNOKEY=/path/to/gnokey, or build one from gnolang/gno at commit"
  echo "9c8eb132e:  CGO_ENABLED=0 go build -o gnokey ./gno.land/cmd/gnokey"
  exit 1
fi
echo "Using $(basename "$GNOKEY")"

n=0
for f in "$HERE"/signed-[1-6]-*.tx; do
  [ -e "$f" ] || { echo "no signed-*.tx files here -- run 1-sign-OFFLINE.sh first"; exit 1; }
  n=$((n + 1))
  printf '[%s/6] %s\n' "$n" "$(basename "$f")"
  if ! "$GNOKEY" broadcast -remote "$REMOTE" "$f"; then
    echo
    echo "FAILED on $(basename "$f"). Nothing after it was sent."
    echo "The remaining files are signed for LATER sequences, so they stay valid"
    echo "only if this one eventually lands. Fix and re-run."
    exit 1
  fi
  echo
done

echo "All six broadcast."
echo "  https://gno.land/r/$NS/kourt"
