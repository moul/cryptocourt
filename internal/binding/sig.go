package binding

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
)

// PROVING A MODERATOR IS THE MODERATOR.
//
// The chain says whether an ADDRESS may publish a court's server. It cannot say
// whether the person at the keyboard controls that address, and that is the gap
// this file closes: a signature over a challenge that names the court, the guild
// and a single-use nonce this service minted.
//
// WHY A SIGNATURE AND NOT AN ON-CHAIN WRITE. The tempting alternative is to have
// the moderator exercise a mod-only power — SetCourtDesc, say — with the nonce in
// it, and read it back. That proves more: it proves the chain accepts them as a
// moderator right now, atomically with the check, rather than proving key control
// and asking the chain separately. It was not taken because the only mod-only
// text field a court has is its DESCRIPTION, which readers see; a verification
// ceremony that requires vandalising the court's own page and then undoing it is
// worse than the thing it protects. If the realm ever grows a scratch field, this
// decision is worth revisiting — the signature path would become unnecessary.
//
// The signature is over the PLAIN TEXT of guild.ChallengeText, not a hash and not
// a transaction. That means any wallet with an "sign this message" affordance can
// produce one, and a person can read what they are agreeing to before they agree
// to it — which matters, because what they are agreeing to is publishing an
// outbound link under a court's name.

var (
	ErrBadPubKey    = errors.New("binding: the public key could not be read")
	ErrBadSignature = errors.New("binding: the signature is not valid for that challenge")
	ErrWrongSigner  = errors.New("binding: the signature is valid but belongs to another address")
)

// Proof is what a claimant sends: which address they say they are, and a
// signature over the challenge that proves it.
//
// PubKey and Signature are standard base64. The address is carried separately and
// redundantly — it is derivable from the pubkey — because a caller that sends
// both is stating an intent this code can check, and a mismatch is a much clearer
// failure than silently binding whatever address the key happened to derive to.
type Proof struct {
	Address   string
	PubKey    string
	Signature string
}

// VerifyProof checks a proof against a challenge and returns the address it
// establishes.
//
// THE ORDER OF CHECKS IS DELIBERATE. The signature is verified against the
// challenge FIRST, and only then is the derived address compared to the claimed
// one. Reversed, a caller could learn whether an address is interesting before
// producing any valid signature at all.
//
// It establishes control of an address and NOTHING ELSE. Whether that address may
// publish this court is a separate question for the chain, asked afterwards,
// because the two answers change on different schedules: a key stays a key, and a
// moderator set does not.
func VerifyProof(challenge string, p Proof) (string, error) {
	if !addrRe.MatchString(p.Address) {
		return "", fmt.Errorf("%w: %q", ErrBadAddress, p.Address)
	}
	pub, err := decodePubKey(p.PubKey)
	if err != nil {
		return "", err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.Signature))
	if err != nil {
		return "", fmt.Errorf("%w: not base64: %v", ErrBadSignature, err)
	}
	if !pub.VerifyBytes([]byte(challenge), sig) {
		return "", ErrBadSignature
	}
	got := pub.Address().String()
	if got != p.Address {
		return "", fmt.Errorf("%w: the key is %s, the claim named %s", ErrWrongSigner, got, p.Address)
	}
	return got, nil
}

// decodePubKey reads a base64 secp256k1 public key in either of the two shapes
// that reach it.
//
// TWO SHAPES, BECAUSE TM2 ITSELF PRODUCES BOTH and a claimant will copy whichever
// their tool gave them. crypto.PubKey.Bytes() is AMINO-encoded — 58 bytes for
// secp256k1, carrying a type prefix — and that is what gnokey and anything using
// std.Signature emit. The bare compressed key is 33 bytes, and that is what a
// browser wallet or a raw crypto library gives you. Accepting only one of them
// was the first version of this function, and it rejected a genuine key with a
// byte count for an error message.
//
// SECP256K1 ONLY. The amino path can decode an ed25519 key perfectly well, so the
// concrete type is checked after decoding rather than assumed: gno accounts are
// secp256k1, and a second scheme here would mean a second address-derivation path
// to keep in step with the chain's.
func decodePubKey(s string) (crypto.PubKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("%w: not base64: %v", ErrBadPubKey, err)
	}

	if len(raw) == secp256k1.PubKeySecp256k1Size {
		var pub secp256k1.PubKeySecp256k1
		copy(pub[:], raw)
		return pub, nil
	}

	decoded, err := crypto.PubKeyFromBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %d bytes, and not an amino-encoded key either: %v",
			ErrBadPubKey, len(raw), err)
	}
	pub, ok := decoded.(secp256k1.PubKeySecp256k1)
	if !ok {
		return nil, fmt.Errorf("%w: it is a %T; gno accounts are secp256k1", ErrBadPubKey, decoded)
	}
	return pub, nil
}
