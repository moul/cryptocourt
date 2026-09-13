#!/usr/bin/env bash
# Assemble and build minsign, the offline signer.
#
#   GNO=~/gopath/src/github.com/gnolang/gno ./tools/minsign/build.sh [outdir]
#
# WHY THIS SCRIPT EXISTS RATHER THAN A COMMITTED MODULE. minsign is 66 Go files,
# and 64 of them are gno's own packages copied in. Committing those would fork
# gno into this repo and rot silently. The two files that are ours live beside
# this script; everything else is derived, and this is the derivation.
#
# WHY minsign EXISTS AT ALL. gnokey links an RPC client, which pulls net/http ->
# crypto/tls -> crypto/x509, and on darwin crypto/x509 binds
# SecTrustCopyCertificateChain — a macOS 12 symbol, bound eagerly, so gnokey
# will not LAUNCH on macOS 10 or 11 even though signing never opens a socket.
# minsign imports no network package, so the symbol never appears and the binary
# runs on an old air-gapped Mac. It also cannot phone home: the code to open a
# socket is not in it.
#
# THE THREE PATCHES BELOW ARE NOT STYLE. Each is forced, and each is verified by
# comparing minsign's output against stock gnokey byte for byte:
#
#   1. strings.SplitSeq -> strings.Split. SplitSeq is go1.24+, and we must build
#      with go1.22 (see below). Same tokens, same order.
#   2. btcec v2.5.0 -> v2.3.2, whose ecdsa.SignCompact returns (sig, error)
#      rather than sig. v2.5.0's go.mod requires go >= 1.25, and Go 1.25 stamps
#      minos 12.0 into every darwin binary it links — which is the floor we are
#      here to get under. Same RFC6979-deterministic ECDSA either way.
#   3. go 1.22, because it stamps minos 10.13 on darwin/amd64. Go 1.23 stamps
#      11.0, Go 1.25 stamps 12.0.
#
# THE CHECK THAT MAKES THOSE SAFE, and it is the only reason to trust any of it:
# signatures here are deterministic, so the same key over the same message MUST
# produce the same bytes. Verify with tools/minsign/verify.sh before use.
set -eu

GNO="${GNO:-$HOME/gopath/src/github.com/gnolang/gno}"
OUT="${1:-$(mktemp -d -t minsign-build)}"
HERE="$(cd "$(dirname "$0")" && pwd)"
GOROOT_122="${GOROOT_122:-$HOME/sdk/go1.22.12}"

[ -d "$GNO/tm2/pkg/amino" ] || { echo "no gno tree at $GNO (set GNO=...)" >&2; exit 1; }
[ -x "$GOROOT_122/bin/go" ] || {
  echo "no go1.22 at $GOROOT_122. Install with:" >&2
  echo "  go install golang.org/dl/go1.22.12@latest && go1.22.12 download" >&2
  exit 1
}

echo "assembling into $OUT"
rm -rf "$OUT"; mkdir -p "$OUT/internal/crypto" "$OUT/internal/amino"

# EXACTLY the packages minsign reaches, no more. Copying tm2/pkg wholesale drags
# in crypto/keys, which needs openpgp, which needs a go1.26 x/crypto.
cp "$GNO"/tm2/pkg/amino/*.go            "$OUT/internal/amino/"
mkdir -p "$OUT/internal/amino/pkg" && cp "$GNO"/tm2/pkg/amino/pkg/*.go "$OUT/internal/amino/pkg/"
for d in errors bech32 overflow std; do
  mkdir -p "$OUT/internal/$d"; cp "$GNO/tm2/pkg/$d"/*.go "$OUT/internal/$d/" 2>/dev/null || true
done
cp "$GNO"/tm2/pkg/crypto/*.go "$OUT/internal/crypto/"
for d in tmhash bip39 hd secp256k1 ed25519 multisig multisig/bitarray; do
  mkdir -p "$OUT/internal/crypto/$d"
  cp "$GNO/tm2/pkg/crypto/$d"/*.go "$OUT/internal/crypto/$d/" 2>/dev/null || true
done
# STRIP THE IGNORE TAG. In this repo these carry `//go:build ignore` so that
# `go vet ./...` does not type-check them against imports that only exist
# once assembled. Here they become the real package, so the tag comes off.
strip_tag() { perl -0pe 's{\A//go:build ignore\n\n(?://[^\n]*\n)*\n}{}' "$1"; }
strip_tag "$HERE/main.go" > "$OUT/main.go"
mkdir -p "$OUT/wiregno" && strip_tag "$HERE/wiregno/gno.go" > "$OUT/wiregno/gno.go"

find "$OUT" -name '*_test.go' -delete
find "$OUT" -name '*.go' -print0 | xargs -0 perl -pi -e \
  's{github\.com/gnolang/gno/tm2/pkg/}{minsign/internal/}g;
   s{github\.com/gnolang/gno/cmd-minsign/wiregno}{minsign/wiregno}g'

# PATCH 1: SplitSeq is go1.24+. Every use is `for x := range SplitSeq(a, b)`,
# which is `for _, x := range Split(a, b)` — same tokens, same order.
# TWO FORMS, and missing the second is a type error rather than a silent bug:
#   inline     for x := range strings.SplitSeq(a, b)
#   two-step   xs := strings.SplitSeq(a, b) ... for x := range xs
# SplitSeq yields VALUES; Split returns a slice, so ranging it yields INDICES.
# The inline rewrite is local, but the two-step one has to find the loop over
# the variable that was assigned — amino/codec.go has exactly this shape, and a
# patch that handled only the inline form compiled to
# `invalid operation: aminoTag == "reserved" (mismatched types int and string)`.
grep -rl 'strings\.SplitSeq' "$OUT" --include='*.go' 2>/dev/null | while read -r f; do
  # names assigned from SplitSeq, before the call itself is rewritten
  vars=$(perl -ne 'print "$1\n" if /^\s*(\w+)\s*:=\s*strings\.SplitSeq\(/' "$f" | sort -u)
  perl -0pi -e 's/for (\w+) := range strings\.SplitSeq\(/for _, $1 := range strings.Split(/g' "$f"
  perl -0pi -e 's/strings\.SplitSeq\(/strings.Split(/g' "$f"
  for v in $vars; do
    perl -0pi -e "s/for (\\w+) := range \\Q$v\\E \\{/for _, \$1 := range $v {/g" "$f"
  done
done

# PATCH 2: btcec v2.3.2 returns (sig, error) where v2.5.0 returns sig alone.
perl -0pi -e 's{\tsig := ecdsa\.SignCompact\(priv, crypto\.Sha256\(msg\), false\)( // [^\n]*)?\n}{\tsig, err := ecdsa.SignCompact(priv, crypto.Sha256(msg), false)\n\tif err != nil \{\n\t\treturn nil, err\n\t\}\n}' \
  "$OUT/internal/crypto/secp256k1/secp256k1_nocgo.go"

printf 'module minsign\n\ngo 1.22\n' > "$OUT/go.mod"

export GOROOT="$GOROOT_122" PATH="$GOROOT_122/bin:$PATH" GOTOOLCHAIN=local
cd "$OUT"
# Versions that predate the go1.25/go1.26 floors their newer releases carry.
go get golang.org/x/crypto@v0.21.0 google.golang.org/protobuf@v1.33.0 \
       github.com/valyala/bytebufferpool@v1.0.0 \
       github.com/btcsuite/btcd/btcec/v2@v2.3.2 \
       github.com/btcsuite/btcd/btcutil@v1.1.5 \
       github.com/decred/dcrd/dcrec/secp256k1/v4@v4.2.0 >/dev/null 2>&1
go mod tidy >/dev/null 2>&1

mkdir -p "$OUT/bin"
for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 freebsd/amd64; do
  os="${t%/*}"; arch="${t#*/}"; ext=""
  [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
    -o "$OUT/bin/minsign-$os-$arch$ext" . || { echo "FAILED $t" >&2; exit 1; }
  printf '  %-20s %s\n' "$os/$arch" "$(wc -c < "$OUT/bin/minsign-$os-$arch$ext" | tr -d ' ') bytes"
done

echo
echo "binaries in $OUT/bin"
echo "darwin/amd64 minimum macOS version (must read 10.13):"
command -v otool >/dev/null && otool -l "$OUT/bin/minsign-darwin-amd64" | grep -m1 minos || true
