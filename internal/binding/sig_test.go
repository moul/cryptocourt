package binding

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/gnolang/gno/tm2/pkg/crypto/ed25519"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
	"github.com/jaekwon/kourt/internal/guild"
)

// A signer, built the way a wallet builds one, so these tests exercise the real
// curve rather than a stub that agrees with itself.
type signer struct {
	priv secp256k1.PrivKeySecp256k1
	addr string
	pub  string
}

func newSigner(t *testing.T) signer {
	t.Helper()
	priv := secp256k1.GenPrivKey()
	pub := priv.PubKey()
	return signer{
		priv: priv,
		addr: pub.Address().String(),
		pub:  base64.StdEncoding.EncodeToString(pub.Bytes()),
	}
}

func (s signer) prove(t *testing.T, challenge string) Proof {
	t.Helper()
	sig, err := s.priv.Sign([]byte(challenge))
	if err != nil {
		t.Fatal(err)
	}
	return Proof{
		Address:   s.addr,
		PubKey:    s.pub,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
}

func challengeFor(t *testing.T, court, guildID string) string {
	t.Helper()
	st := guild.State{Chain: "kourt-1", Court: court, Nonce: strings.Repeat("ab", 16)}
	c, err := guild.ChallengeText(st, guildID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const testGuild = "1478455953715236886"

func TestAGenuineProofEstablishesItsAddress(t *testing.T) {
	s := newSigner(t)
	ch := challengeFor(t, "meta", testGuild)

	got, err := VerifyProof(ch, s.prove(t, ch))
	if err != nil {
		t.Fatalf("a genuine proof was refused: %v", err)
	}
	if got != s.addr {
		t.Errorf("established %s, want %s", got, s.addr)
	}
	// And the address it establishes is one the rest of this package will accept
	// — the two regexes agreeing is not automatic.
	if !addrRe.MatchString(got) {
		t.Errorf("a real key derived %q, which addrRe rejects", got)
	}
}

// THE CHALLENGE BINDS COURT, GUILD AND NONCE, AND THIS IS WHERE THAT PAYS OFF.
// A signature is a warrant for exactly one of each; every row below is a
// signature that is perfectly valid for something else.
func TestAProofDoesNotTransferToAnotherChallenge(t *testing.T) {
	s := newSigner(t)
	mine := challengeFor(t, "meta", testGuild)
	p := s.prove(t, mine)

	others := map[string]string{
		"another court": challengeFor(t, "covid", testGuild),
		"another guild": challengeFor(t, "meta", "1478455953715236887"),
		"another nonce": func() string {
			st := guild.State{Chain: "kourt-1", Court: "meta", Nonce: strings.Repeat("cd", 16)}
			c, _ := guild.ChallengeText(st, testGuild)
			return c
		}(),
		"another chain": func() string {
			st := guild.State{Chain: "dev", Court: "meta", Nonce: strings.Repeat("ab", 16)}
			c, _ := guild.ChallengeText(st, testGuild)
			return c
		}(),
	}
	for name, ch := range others {
		if _, err := VerifyProof(ch, p); !errors.Is(err, ErrBadSignature) {
			t.Errorf("a proof for one challenge verified against %s: %v", name, err)
		}
	}
	// The pairing: it still verifies against its own.
	if _, err := VerifyProof(mine, p); err != nil {
		t.Errorf("the proof stopped verifying against its own challenge: %v", err)
	}
}

// SOMEBODY ELSE'S VALID SIGNATURE IS STILL SOMEBODY ELSE'S. The claimed address
// is redundant with the key, and this is why it is carried anyway: without the
// comparison, a proof would bind whatever address the key derived to, which is
// not necessarily the one the request said it was for.
func TestASignatureFromAnotherKeyIsRefusedEvenThoughItIsValid(t *testing.T) {
	mallory, victim := newSigner(t), newSigner(t)
	ch := challengeFor(t, "meta", testGuild)

	p := mallory.prove(t, ch)
	p.Address = victim.addr // valid signature, someone else's name on it

	_, err := VerifyProof(ch, p)
	if !errors.Is(err, ErrWrongSigner) {
		t.Fatalf("a valid signature under a claimed foreign address gave %v", err)
	}
	if !strings.Contains(err.Error(), mallory.addr) || !strings.Contains(err.Error(), victim.addr) {
		t.Errorf("the error names neither key: %v", err)
	}
}

func TestAMismatchedPubKeyIsRefused(t *testing.T) {
	a, b := newSigner(t), newSigner(t)
	ch := challengeFor(t, "meta", testGuild)

	p := a.prove(t, ch)
	p.PubKey = b.pub // the signature no longer matches the key it is checked with
	if _, err := VerifyProof(ch, p); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a signature checked against the wrong key gave %v", err)
	}
}

func TestMalformedProofsAreRefusedWithoutPanicking(t *testing.T) {
	s := newSigner(t)
	ch := challengeFor(t, "meta", testGuild)
	good := s.prove(t, ch)

	bad := []struct {
		name string
		p    Proof
		want error
	}{
		{"no address", Proof{"", good.PubKey, good.Signature}, ErrBadAddress},
		{"junk address", Proof{"notanaddress", good.PubKey, good.Signature}, ErrBadAddress},
		{"no pubkey", Proof{s.addr, "", good.Signature}, ErrBadPubKey},
		{"pubkey not base64", Proof{s.addr, "!!!!", good.Signature}, ErrBadPubKey},
		{"pubkey wrong length", Proof{s.addr, base64.StdEncoding.EncodeToString([]byte("short")), good.Signature}, ErrBadPubKey},
		{"no signature", Proof{s.addr, good.PubKey, ""}, ErrBadSignature},
		{"signature not base64", Proof{s.addr, good.PubKey, "!!!!"}, ErrBadSignature},
		{"signature truncated", Proof{s.addr, good.PubKey, good.Signature[:20]}, ErrBadSignature},
	}
	for _, c := range bad {
		got, err := VerifyProof(ch, c.p)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
		if got != "" {
			t.Errorf("%s: established %q alongside an error", c.name, got)
		}
	}
	// The pairing that keeps the table honest.
	if _, err := VerifyProof(ch, good); err != nil {
		t.Errorf("the good proof was refused: %v", err)
	}
}

// Whitespace around a pasted key or signature is what a person actually produces
// when copying out of a terminal, and refusing it would be a support burden with
// no security value.
func TestPastedWhitespaceIsTolerated(t *testing.T) {
	s := newSigner(t)
	ch := challengeFor(t, "meta", testGuild)
	p := s.prove(t, ch)
	p.PubKey = "  " + p.PubKey + "\n"
	p.Signature = "\t" + p.Signature + "  \n"
	if _, err := VerifyProof(ch, p); err != nil {
		t.Errorf("a proof with copy-paste whitespace was refused: %v", err)
	}
}

// An empty challenge must not verify against anything. It is the value a caller
// gets from a bug — a nonce that failed to load, a court that came back blank —
// and a signature over "" would otherwise be a universal key.
func TestAnEmptyChallengeCannotBeSatisfiedByAProofForARealOne(t *testing.T) {
	s := newSigner(t)
	ch := challengeFor(t, "meta", testGuild)
	if _, err := VerifyProof("", s.prove(t, ch)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a real proof satisfied an empty challenge: %v", err)
	}
}

// BOTH ENCODINGS, because tm2 produces both and a claimant copies whichever their
// tool gave them. PubKey.Bytes() is amino (58 bytes, type-prefixed) — what gnokey
// and std.Signature emit; the bare array is the 33-byte compressed key — what a
// browser wallet or a raw crypto library gives you. The first version of the
// decoder took only one and rejected a genuine key with a byte count.
func TestBothPublicKeyEncodingsAreAccepted(t *testing.T) {
	s := newSigner(t)
	ch := challengeFor(t, "meta", testGuild)
	p := s.prove(t, ch)

	pub := s.priv.PubKey().(secp256k1.PubKeySecp256k1)
	amino := base64.StdEncoding.EncodeToString(pub.Bytes())
	bare := base64.StdEncoding.EncodeToString(pub[:])
	if len(pub.Bytes()) == len(pub[:]) {
		t.Fatal("the two encodings are the same length; this test no longer " +
			"distinguishes them")
	}

	for name, key := range map[string]string{"amino": amino, "bare": bare} {
		q := p
		q.PubKey = key
		got, err := VerifyProof(ch, q)
		if err != nil {
			t.Errorf("%s encoding refused: %v", name, err)
			continue
		}
		if got != s.addr {
			t.Errorf("%s encoding established %s, want %s", name, got, s.addr)
		}
	}
}

// The amino path can decode an ed25519 key perfectly well, so the concrete type
// is checked rather than assumed. A gno account is secp256k1; anything else
// deriving an address here would be a second derivation path to keep in step.
func TestAnEd25519KeyIsRefusedEvenThoughItDecodes(t *testing.T) {
	priv := ed25519.GenPrivKey()
	enc := base64.StdEncoding.EncodeToString(priv.PubKey().Bytes())
	sig, err := priv.Sign([]byte("anything"))
	if err != nil {
		t.Fatal(err)
	}
	p := Proof{
		Address:   priv.PubKey().Address().String(),
		PubKey:    enc,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	if _, err := VerifyProof("anything", p); !errors.Is(err, ErrBadPubKey) {
		t.Errorf("an ed25519 key was not refused as a key: %v", err)
	}
}
