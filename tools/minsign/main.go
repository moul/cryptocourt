// minsign composes and signs a gno addpkg transaction, and does nothing else.
//
// WHY THIS EXISTS. gnokey links an RPC client, which pulls net/http ->
// crypto/tls -> crypto/x509, and on darwin crypto/x509 binds
// SecTrustCopyCertificateChain -- a macOS 12 symbol, bound eagerly in
// __DATA_CONST.__got, so the binary refuses to LAUNCH on anything older even
// though signing never opens a socket. Nothing here imports a network package,
// so that symbol never appears.
//
// It is deliberately one readable file. The mnemonic is read from stdin, so it
// is never an argument and cannot reach ps output or a shell history; it is
// held only in memory. The only output is the signed transaction.
//
// DO NOT TRUST THIS FILE ON ITS OWN. Its output is verified against real
// gnokey's, byte for byte, by the deploy script -- see the check in
// 1-sign-OFFLINE.sh. That comparison, not this comment, is the guarantee.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"minsign/internal/amino"
	"minsign/internal/crypto"
	"minsign/internal/crypto/bip39"
	"minsign/internal/crypto/hd"
	"minsign/internal/crypto/secp256k1"
	"minsign/internal/std"

	"minsign/wiregno"
)

// MsgAddPackage is declared here rather than imported from
// gno.land/pkg/sdk/vm, and that one import is the whole reason this file
// exists: that package is the VM keeper, and it transitively pulls in
// crypto/x509.
//
// Only the WIRE SHAPE has to match, not the behaviour. The signature covers
// sortJSON(aminoJSON(SignDoc)), so what matters is the amino type name
// ("/vm.m_addpkg") and the four JSON field names -- both copied verbatim from
// upstream. ValidateBasic is a stub because the chain runs its own; a local
// check that disagreed would only reject transactions the chain would take.
type MsgAddPackage struct {
	Creator    crypto.Address  `json:"creator" yaml:"creator"`
	Package    *std.MemPackage `json:"package" yaml:"package"`
	Send       std.Coins       `json:"send" yaml:"send"`
	MaxDeposit std.Coins       `json:"max_deposit" yaml:"max_deposit"`
}

func (msg MsgAddPackage) Route() string        { return "vm" }
func (msg MsgAddPackage) Type() string         { return "add_package" }
func (msg MsgAddPackage) ValidateBasic() error { return nil }

func (msg MsgAddPackage) GetSignBytes() []byte {
	return std.MustSortJSON(amino.MustMarshalJSON(msg))
}

func (msg MsgAddPackage) GetSigners() []crypto.Address {
	return []crypto.Address{msg.Creator}
}

// The amino package NAME -- the second argument -- is "vm", and that is what
// makes the encoded type "/vm.m_addpkg", identical to upstream's. The FIRST
// argument is the Go import path, which never reaches the wire; amino asserts
// it equals the registered type's real package, so it must say "main" here and
// not the upstream path. Getting this wrong is a startup panic, not a silent
// wire change.
var vmPackage = amino.RegisterPackage(amino.NewPackage(
	"main",
	"vm",
	amino.GetCallersDirname(),
).WithDependencies(
	std.Package,
).WithTypes(
	MsgAddPackage{}, "m_addpkg",
	MsgCall{}, "m_call",
))


// MsgCall is the seeding message: StartCourt, OpenClaim, folder moves. Declared
// beside MsgAddPackage for the same reason and registered in the same amino
// package, so both encode with the "/vm." prefix the chain expects.
//
// Type() is "exec", not "call" -- copied from upstream rather than guessed,
// because the string reaches the chain's router.
type MsgCall struct {
	Caller     crypto.Address `json:"caller" yaml:"caller"`
	Send       std.Coins      `json:"send" yaml:"send"`
	MaxDeposit std.Coins      `json:"max_deposit" yaml:"max_deposit"`
	PkgPath    string         `json:"pkg_path" yaml:"pkg_path"`
	Func       string         `json:"func" yaml:"func"`
	Args       []string       `json:"args,omitempty" yaml:"args"`
}

func (msg MsgCall) Route() string        { return "vm" }
func (msg MsgCall) Type() string         { return "exec" }
func (msg MsgCall) ValidateBasic() error { return nil }

func (msg MsgCall) GetSignBytes() []byte {
	return std.MustSortJSON(amino.MustMarshalJSON(msg))
}

func (msg MsgCall) GetSigners() []crypto.Address {
	return []crypto.Address{msg.Caller}
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "minsign: "+f+"\n", a...)
	os.Exit(1)
}

// readMemPackage collects the .gno and .toml files of one directory, in the
// order os.ReadDir returns them -- which is sorted by name. Sorted matters:
// the signature covers these bytes, so an unstable order would produce a
// different signed document on every run.
func readMemPackage(dir, pkgPath string) *std.MemPackage {
	ents, err := os.ReadDir(dir)
	if err != nil {
		die("reading %s: %v", dir, err)
	}
	// Type is what gnokey stamps and the chain reads back. Leaving it nil
	// produced a document 59 bytes shorter than gnokey's -- which is a
	// different signature over different bytes, not a cosmetic difference.
	mp := &std.MemPackage{Path: pkgPath, Type: wiregno.MPUserAll}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".gno") && !strings.HasSuffix(n, ".toml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			die("reading %s: %v", n, err)
		}
		body := string(b)
		mp.Files = append(mp.Files, &std.MemFile{Name: n, Body: body})
		// The package name is the `package` clause, which the chain checks
		// against the last path element. Read from the first .gno file that
		// declares one, never from the directory name -- retargeting renames
		// directories and the two can differ.
		if mp.Name == "" && strings.HasSuffix(n, ".gno") {
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(line, "package ") {
					mp.Name = strings.TrimSpace(strings.TrimPrefix(line, "package "))
					break
				}
			}
		}
	}
	if len(mp.Files) == 0 {
		die("no .gno or .toml files in %s", dir)
	}
	if mp.Name == "" {
		die("no `package` clause found in %s", dir)
	}
	return mp
}


// planStep is one row of scripts/mainnet-plan.py's output. That script decides
// WHICH calls a locked chain can take; this one only signs what it is given.
type planStep struct {
	Who  string   `json:"who"`
	Func string   `json:"func"`
	Args []string `json:"args"`
	Send string   `json:"send"`
}

// actor is one signer: which HD index derives it, and where its own sequence
// counter stands. EVERY SIGNER HAS ITS OWN SEQUENCE -- the chain counts per
// account, not per batch -- so one ascending counter across a mixed plan signs
// all but the first actor's calls for a sequence the chain will never reach.
type actor struct {
	Index         uint32 `json:"index"`
	AccountNumber int64  `json:"account_number"`
	Sequence      int64  `json:"sequence"`
}

// signPlan signs a whole seeding plan in one pass, sequences ascending from
// startSeq. One pass rather than one invocation per call because the mnemonic
// is typed once: forty-odd separate runs means forty-odd chances to type it
// somewhere that is not this program.
//
// A file per call, not one bundle, because the chain takes them one at a time
// and a failure part-way has to leave the rest broadcastable.

// keyFromStdin reads the mnemonic and derives the key. Shared by both modes so
// there is one place the path 44'/118'/0'/0/0 is written down -- a different
// path derives a different address in silence, and the only thing standing
// between that and a wasted batch is the address printed here.
// keyAt derives one actor from the shared seed. THE SCENARIO HAS SEVENTEEN
// SIGNERS and they are not seventeen secrets: BIP44 gives each an index off the
// same mnemonic, so the offline machine holds one phrase and can produce them
// all. Index 0 is the deployer, which is the address the realm made admin.
func keyAt(mnemonic string, index uint32) secp256k1.PrivKeySecp256k1 {
	seed := bip39.NewSeed(mnemonic, "")
	master, ch := hd.ComputeMastersFromSeed(seed)
	path := fmt.Sprintf("44'/118'/0'/0/%d", index)
	derived, err := hd.DerivePrivateKeyForPath(master, ch, path)
	if err != nil {
		die("deriving %s: %v", path, err)
	}
	return secp256k1.PrivKeySecp256k1(derived)
}

// mnemonicFromStdin reads the phrase and nothing else. Separate from derivation
// so the multi-actor path does not read stdin seventeen times.
func mnemonicFromStdin() string {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 4096), 4096)
	if !sc.Scan() {
		die("no mnemonic on stdin")
	}
	m := strings.TrimSpace(sc.Text())
	if !bip39.IsMnemonicValid(m) {
		die("that is not a valid BIP39 mnemonic")
	}
	return m
}

func keyFromStdin() secp256k1.PrivKeySecp256k1 {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 4096), 4096)
	if !sc.Scan() {
		die("no mnemonic on stdin")
	}
	priv := keyAt(strings.TrimSpace(sc.Text()), 0)
	fmt.Fprintf(os.Stderr, "  signer  %s\n", priv.PubKey().Address())
	return priv
}


// parseUgnot turns the scenario's "123ugnot" into coins. Empty means no send,
// which is every call the locked chain can take; after the unlock Buy carries
// one and it is the only thing the token lock ever refused.
func parseUgnot(s string) std.Coins {
	if s == "" {
		return nil
	}
	if !strings.HasSuffix(s, "ugnot") {
		die("send %q is not in ugnot", s)
	}
	var n int64
	if _, err := fmt.Sscan(strings.TrimSuffix(s, "ugnot"), &n); err != nil {
		die("send %q has no amount", s)
	}
	return std.Coins{std.Coin{Denom: "ugnot", Amount: n}}
}

func signPlan(mnemonic, planPath, actorsPath, pkgPath, chainID, outDir string,
	gasWanted, gasFee int64) {
	var steps []planStep
	b, err := os.ReadFile(planPath)
	if err != nil {
		die("reading plan: %v", err)
	}
	if err := json.Unmarshal(b, &steps); err != nil {
		die("plan is not the JSON mainnet-plan.py emits: %v", err)
	}
	ab, err := os.ReadFile(actorsPath)
	if err != nil {
		die("reading actors: %v", err)
	}
	actors := map[string]*actor{}
	if err := json.Unmarshal(ab, &actors); err != nil {
		die("actors file is not name -> {index, account_number, sequence}: %v", err)
	}
	// THE ACTORS FILE MUST BELONG TO THIS PLAN. Indices are assigned by first
	// appearance, so the same name gets a different index — and a different
	// address — out of a different plan. Measured: `foia` is index 0 from a
	// five-row subset and index 6 from the full docket. Sign one plan with the
	// other's actors file and every call is signed from an address that holds
	// nothing, which shows up only at broadcast, after the air gap.
	//
	// Recomputing the order here is cheap and decisive: it is the same rule
	// `addrs` used, so agreement proves the two came from the same plan.
	want := map[string]uint32{}
	var n uint32
	for _, st := range steps {
		if st.Who != "" {
			if _, seen := want[st.Who]; !seen {
				want[st.Who] = n
				n++
			}
		}
	}
	for name, a := range actors {
		w, ok := want[name]
		if !ok {
			die("actors file names %q, which this plan never uses — it was built "+
				"from a different plan", name)
		}
		if w != a.Index {
			die("actor %q has index %d in the actors file and %d in this plan: the "+
				"file was built from a different plan, and every signature would "+
				"come from an address that holds nothing", name, a.Index, w)
		}
	}

	// Derive every actor up front and PRINT the addresses. A wrong index is a
	// valid signature from an account that holds nothing, and it fails only at
	// broadcast -- after the whole batch has crossed the air gap.
	keys := map[string]secp256k1.PrivKeySecp256k1{}
	for name, a := range actors {
		k := keyAt(mnemonic, a.Index)
		keys[name] = k
		fmt.Fprintf(os.Stderr, "  actor %-14s index=%-3d acct=%-8d seq=%-5d %s\n",
			name, a.Index, a.AccountNumber, a.Sequence, k.PubKey().Address())
	}
	for _, st := range steps {
		if _, ok := actors[st.Who]; !ok {
			die("plan needs signer %q, which the actors file does not name", st.Who)
		}
	}
	for i, st := range steps {
		a := actors[st.Who]
		priv := keys[st.Who]
		addr := priv.PubKey().Address()
		seq := a.Sequence
		a.Sequence++
		accNum := a.AccountNumber
		tx := std.Tx{
			Msgs: []std.Msg{MsgCall{
				Caller:  addr,
				PkgPath: pkgPath,
				Func:    st.Func,
				Args:    st.Args,
				Send:    parseUgnot(st.Send),
			}},
			Fee: std.Fee{GasWanted: gasWanted,
				GasFee: std.Coin{Denom: "ugnot", Amount: gasFee}},
		}
		signBytes, err := tx.GetSignBytes(chainID, uint64(accNum), uint64(seq))
		if err != nil {
			die("sign bytes for step %d (%s): %v", i, st.Func, err)
		}
		sig, err := priv.Sign(signBytes)
		if err != nil {
			die("signing step %d: %v", i, err)
		}
		tx.Signatures = []std.Signature{{PubKey: priv.PubKey(), Signature: sig}}
		out, err := amino.MarshalJSON(tx)
		if err != nil {
			die("encoding step %d: %v", i, err)
		}
		name := fmt.Sprintf("%s/call-%03d-%s-%s.tx", outDir, i+1, st.Who, st.Func)
		if err := os.WriteFile(name, out, 0o600); err != nil {
			die("writing %s: %v", name, err)
		}
		fmt.Fprintf(os.Stderr, "  [%3d/%d] %-12s seq=%-4d %-22s -> %s\n",
			i+1, len(steps), st.Who, seq, st.Func, filepath.Base(name))
	}
	fmt.Fprintf(os.Stderr, "  %d call(s) signed across %d signer(s)\n", len(steps), len(actors))
}

func main() {
	_ = vmPackage

	// ADDRS: the offline half of provisioning. Only the seed can say what
	// address an index derives, and only the chain can say what account number
	// that address was given — so the two have to meet in the middle. This
	// emits name -> {index, address}; the online side fills in account_number
	// and sequence and hands back the actors file `plan` reads.
	//
	// Index order is FIRST APPEARANCE IN THE PLAN, not alphabetical: it has to
	// be reproducible from the plan alone, so that regenerating this file after
	// an edit does not silently renumber actors that are already funded.
	if len(os.Args) > 1 && os.Args[1] == "addrs" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: minsign addrs <plan.json>")
			os.Exit(2)
		}
		b, err := os.ReadFile(os.Args[2])
		if err != nil {
			die("reading plan: %v", err)
		}
		var steps []planStep
		if err := json.Unmarshal(b, &steps); err != nil {
			die("plan is not the JSON mainnet-plan.py emits: %v", err)
		}
		mnemonic := mnemonicFromStdin()
		seen := map[string]bool{}
		out := map[string]map[string]any{}
		var order []string
		for _, st := range steps {
			if st.Who != "" && !seen[st.Who] {
				seen[st.Who] = true
				order = append(order, st.Who)
			}
		}
		for i, name := range order {
			k := keyAt(mnemonic, uint32(i))
			out[name] = map[string]any{
				"index":          i,
				"address":        k.PubKey().Address().String(),
				"account_number": 0,
				"sequence":       0,
			}
			fmt.Fprintf(os.Stderr, "  %-16s index=%-3d %s\n", name, i, k.PubKey().Address())
		}
		enc, err := json.MarshalIndent(out, "", " ")
		if err != nil {
			die("encoding: %v", err)
		}
		os.Stdout.Write(append(enc, '\n'))
		return
	}

	// TWO MODES, and the addpkg one keeps its exact positional interface: the
	// deploy script in the bundle already calls it that way and has been used
	// against mainnet. A new mode is a new first argument, not a reshuffle.
	//
	//   minsign plan <plan.json> <actors.json> <pkgpath> <gas-wanted> <gas-fee> <chainid> <outdir>
	//
	// The account number and sequence are PER ACTOR and live in actors.json,
	// not on the command line: the scenario has seventeen signers and each has
	// its own counter on the chain. They are looked up online, after funding,
	// and carried across with the plan.
	if len(os.Args) > 1 && os.Args[1] == "plan" {
		if len(os.Args) != 9 {
			fmt.Fprintln(os.Stderr,
				"usage: minsign plan <plan.json> <actors.json> <pkgpath> <gas-wanted> <gas-fee> <chainid> <outdir>")
			fmt.Fprintln(os.Stderr,
				"  actors.json: {\"name\": {\"index\": 0, \"account_number\": 0, \"sequence\": 0}, ...}")
			os.Exit(2)
		}
		var gw, gf int64
		for i, p := range []*int64{&gw, &gf} {
			src := []string{os.Args[5], os.Args[6]}[i]
			if _, err := fmt.Sscan(src, p); err != nil {
				die("bad numeric argument %q", src)
			}
		}
		signPlan(mnemonicFromStdin(), os.Args[2], os.Args[3], os.Args[4],
			os.Args[7], os.Args[8], gw, gf)
		return
	}

	if len(os.Args) != 8 {
		fmt.Fprintln(os.Stderr,
			"usage: minsign <pkgdir> <pkgpath> <gas-wanted> <gas-fee-ugnot> <max-deposit-ugnot> <chainid> <accnum:seq>")
		fmt.Fprintln(os.Stderr,
			"the BIP39 mnemonic is read from stdin; the signed tx is written to stdout")
		os.Exit(2)
	}
	pkgDir, pkgPath := os.Args[1], os.Args[2]
	var gasWanted, gasFee, maxDep, accNum, seq int64
	if _, err := fmt.Sscan(os.Args[3], &gasWanted); err != nil {
		die("bad gas-wanted %q", os.Args[3])
	}
	if _, err := fmt.Sscan(os.Args[4], &gasFee); err != nil {
		die("bad gas-fee %q", os.Args[4])
	}
	if _, err := fmt.Sscan(os.Args[5], &maxDep); err != nil {
		die("bad max-deposit %q", os.Args[5])
	}
	chainID := os.Args[6]
	if _, err := fmt.Sscanf(os.Args[7], "%d:%d", &accNum, &seq); err != nil {
		die("expected <account-number>:<sequence>, got %q", os.Args[7])
	}

	priv := keyFromStdin()

	addr := priv.PubKey().Address()
	fmt.Fprintf(os.Stderr, "  signer  %s\n", addr)
	fmt.Fprintf(os.Stderr, "  chain   %s   account %d   sequence %d\n", chainID, accNum, seq)

	mp := readMemPackage(pkgDir, pkgPath)
	fmt.Fprintf(os.Stderr, "  package %s (%d files) -> %s\n", mp.Name, len(mp.Files), pkgPath)

	tx := std.Tx{
		Msgs: []std.Msg{MsgAddPackage{
			Creator:    addr,
			Package:    mp,
			MaxDeposit: std.Coins{std.Coin{Denom: "ugnot", Amount: maxDep}},
		}},
		Fee: std.Fee{
			GasWanted: gasWanted,
			GasFee:    std.Coin{Denom: "ugnot", Amount: gasFee},
		},
	}

	// The same call the chain's ante handler makes to rebuild what it verifies.
	signBytes, err := tx.GetSignBytes(chainID, uint64(accNum), uint64(seq))
	if err != nil {
		die("sign bytes: %v", err)
	}
	sig, err := priv.Sign(signBytes)
	if err != nil {
		die("signing: %v", err)
	}
	tx.Signatures = []std.Signature{{PubKey: priv.PubKey(), Signature: sig}}

	out, err := amino.MarshalJSON(tx)
	if err != nil {
		die("encoding: %v", err)
	}
	os.Stdout.Write(out)
}
