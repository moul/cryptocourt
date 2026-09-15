package binding

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaekwon/kourt/internal/gnorpc"
)

// A real kourt-1 address, and the one this repo's memory records as holding
// moderator rights on meta and covid.
const modAddr = "g174hxvmvs7fsg50gy7xu8x9eyqa4chdv8tclngz"

func verifier(t *testing.T, h http.HandlerFunc) *Verifier {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Verifier{Node: &gnorpc.Node{RPC: srv.URL, HTTP: srv.Client()}}
}

func answers(data string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{"Data":%q}}}}`,
			base64.StdEncoding.EncodeToString([]byte(data)))
	}
}

// sentExpr returns the qeval expression a call actually put on the wire.
func sentExpr(t *testing.T, call func(*Verifier) error) string {
	t.Helper()
	var expr string
	v := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct{ Data string } `json:"params"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		raw, _ := base64.StdEncoding.DecodeString(req.Params.Data)
		expr = string(raw)
		answers("(false bool)")(w, r)
	})
	_ = call(v)
	return expr
}

func TestIsModReadsTheChainsYesAndNo(t *testing.T) {
	yes := verifier(t, answers("(true bool)"))
	got, err := yes.IsMod(context.Background(), "meta", modAddr)
	if err != nil || !got {
		t.Errorf("a true answer read as (%v, %v)", got, err)
	}

	no := verifier(t, answers("(false bool)"))
	got, err = no.IsMod(context.Background(), "meta", modAddr)
	if err != nil || got {
		t.Errorf("a false answer read as (%v, %v)", got, err)
	}
}

// THE PROPERTY THE WHOLE PUBLISH RULE RESTS ON.
//
// "Unparseable therefore not a moderator" is how a realm upgrade that changes a
// return type silently revokes every court's Discord at once, with no error
// anywhere. So anything that is not exactly a bool is an error, and the error is
// distinguishable so the two callers can handle it in opposite directions.
func TestAnUnreadableAnswerIsAnErrorAndNotAQuietNo(t *testing.T) {
	for _, out := range []string{
		"", "(1 int)", `("true" string)`, "true", "(true bool)(false bool)",
		"undefined", "(nil)",
	} {
		v := verifier(t, answers(out))
		got, err := v.IsMod(context.Background(), "meta", modAddr)
		if err == nil {
			t.Errorf("%q was accepted as %v", out, got)
			continue
		}
		if !errors.Is(err, ErrUnreadable) {
			t.Errorf("%q gave %v, which callers cannot tell from a real refusal", out, err)
		}
		if got {
			t.Errorf("%q returned true alongside an error", out)
		}
	}
}

// A node that is down must not look like a court that said no.
func TestAnUnreachableNodeIsAnError(t *testing.T) {
	v := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := v.IsMod(context.Background(), "meta", modAddr); err == nil {
		t.Error("a 502 read as a clean answer")
	}
}

// A court that does not exist panics in the realm, which arrives as a query
// error. It must not read as "you are not a moderator of it".
func TestAMissingCourtIsAnErrorNotAFalse(t *testing.T) {
	v := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{`+
			`"Error":{"value":"kourtv2: no such court"},"Log":"kourtv2: no such court"}}}}`)
	})
	got, err := v.IsMod(context.Background(), "nope", modAddr)
	if err == nil {
		t.Fatalf("a nonexistent court answered %v", got)
	}
	if !strings.Contains(err.Error(), "no such court") {
		t.Errorf("the error loses what the chain said: %v", err)
	}
}

// QEVAL EXPRESSION INJECTION. The address is interpolated into the expression, so
// a quote in it could close the string and append an argument or another call.
// This is the only unvalidated path from a caller to the chain, and %q escaping
// alone is not something to rely on when a regex can make the input impossible.
func TestAnAddressCannotReopenTheExpression(t *testing.T) {
	hostile := []string{
		`g1abc","meta"),AddGlobalMod("`,
		`g1abc"` + `)` + `;`,
		"g1abc\"",
		"g1abc\\",
		"g1abc\n",
		"", "g1", "notanaddress",
		"G174HXVMVS7FSG50GY7XU8X9EYQA4CHDV8TCLNGZ", // uppercase
		modAddr + "x",            // too long
		modAddr[:len(modAddr)-1], // too short
		"g1b" + modAddr[3:],      // 'b' is not in bech32's charset
	}
	for _, a := range hostile {
		v := verifier(t, answers("(true bool)"))
		got, err := v.IsMod(context.Background(), "meta", a)
		if err == nil {
			t.Errorf("%q was accepted, answering %v", a, got)
			continue
		}
		if !errors.Is(err, ErrBadAddress) {
			t.Errorf("%q was refused for the wrong reason: %v", a, err)
		}
	}
	// The pairing that keeps the table honest: a real address still works.
	v := verifier(t, answers("(true bool)"))
	if _, err := v.IsMod(context.Background(), "meta", modAddr); err != nil {
		t.Errorf("a real address was refused: %v", err)
	}
}

func TestACourtSlugCannotReopenTheExpression(t *testing.T) {
	for _, c := range []string{`meta","x`, "meta)", "", "MY-COURT", "my-court", strings.Repeat("a", 12)} {
		v := verifier(t, answers("(true bool)"))
		if _, err := v.IsMod(context.Background(), c, modAddr); !errors.Is(err, ErrBadCourt) {
			t.Errorf("%q was not refused as a court slug: %v", c, err)
		}
	}
}

// The expression on the wire must be the one intended — the escaping is the point
// of the previous two tests, and this is what proves the escaping happens at all.
func TestTheExpressionIsTheOneIntended(t *testing.T) {
	got := sentExpr(t, func(v *Verifier) error {
		_, err := v.IsMod(context.Background(), "meta", modAddr)
		return err
	})
	want := fmt.Sprintf(`%s.IsCourtMod("meta",%q)`, DefaultPkgPath, modAddr)
	if got != want {
		t.Errorf("sent  %s\nwant %s", got, want)
	}
}

func TestModThresholdReadsTwoReturnValues(t *testing.T) {
	v := verifier(t, answers("(3 int)(5 int)"))
	m, n, err := v.ModThreshold(context.Background(), "meta")
	if err != nil {
		t.Fatal(err)
	}
	if m != 3 || n != 5 {
		t.Errorf("got %d-of-%d, want 3-of-5", m, n)
	}

	// Before a set materialises the realm reports 1-of-1, which is a real answer.
	v1 := verifier(t, answers("(1 int)(1 int)"))
	if m, n, err := v1.ModThreshold(context.Background(), "fresh"); err != nil || m != 1 || n != 1 {
		t.Errorf("1-of-1 read as (%d,%d,%v)", m, n, err)
	}
}

// A threshold that cannot be true is a misread, not a value to act on: m>n would
// mean a court whose moderators can never reach their own quorum.
func TestAnImpossibleThresholdIsRefused(t *testing.T) {
	for _, out := range []string{"(5 int)(3 int)", "(0 int)(3 int)", "(3 int)", "", "(a int)(b int)"} {
		v := verifier(t, answers(out))
		if _, _, err := v.ModThreshold(context.Background(), "meta"); err == nil {
			t.Errorf("%q was accepted", out)
		}
	}
}

func TestThePkgPathIsOverridable(t *testing.T) {
	var expr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct{ Data string } `json:"params"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		raw, _ := base64.StdEncoding.DecodeString(req.Params.Data)
		expr = string(raw)
		answers("(true bool)")(w, r)
	}))
	defer srv.Close()

	v := &Verifier{
		Node:    &gnorpc.Node{RPC: srv.URL, HTTP: srv.Client()},
		PkgPath: "gno.land/r/kourt/kourtv3",
	}
	if _, err := v.IsMod(context.Background(), "meta", modAddr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(expr, "gno.land/r/kourt/kourtv3.") {
		t.Errorf("PkgPath was ignored: %s", expr)
	}
}

// A COURT THAT DOES NOT EXIST GETS ITS OWN ERROR, so a caller can say "check the
// slug" instead of printing the realm's multi-line traceback at somebody who
// mistyped six characters.
func TestAMissingCourtIsClassified(t *testing.T) {
	traceback := `--= Error =--
Data: errors.FmtError{format:"kourtv2: no such court", args:[]interface {}(nil)}
Msg Traces:
    @ /gno/r/kourt/kourtv2/court.gno:1191`
	v := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{`+
			`"Error":{"value":"x"},"Log":%q}}}}`, traceback)
	})
	_, err := v.IsMod(context.Background(), "nope", modAddr)
	if !errors.Is(err, ErrNoCourt) {
		t.Errorf("a missing court was not classified: %v", err)
	}
	if strings.Contains(err.Error(), "Msg Traces") {
		t.Errorf("the traceback survived into the classified error: %v", err)
	}

	// And ModThreshold classifies it the same way — one court, one answer,
	// whichever question was asked.
	if _, _, err := v.ModThreshold(context.Background(), "nope"); !errors.Is(err, ErrNoCourt) {
		t.Errorf("ModThreshold did not classify a missing court: %v", err)
	}
}

// Classification must not swallow unrelated failures into "no such court".
func TestOtherRealmErrorsAreNotClassifiedAsAMissingCourt(t *testing.T) {
	v := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{`+
			`"Error":{"value":"x"},"Log":"kourtv2: something else entirely"}}}}`)
	})
	_, err := v.IsMod(context.Background(), "meta", modAddr)
	if err == nil {
		t.Fatal("an unrelated realm error came back clean")
	}
	if errors.Is(err, ErrNoCourt) {
		t.Errorf("an unrelated error was reported as a missing court: %v", err)
	}
}
