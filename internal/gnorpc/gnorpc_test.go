package gnorpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// EVERY FAILURE THIS ENVELOPE CAN HAVE LOOKS LIKE AN EMPTY ANSWER.
//
// That is the reason this file exists rather than the transport being trusted
// because archive works. Mis-handle the base64, the ResponseBase nesting, or the
// query-error field and QEval returns "" with a nil error — which a caller asking
// "is this address a moderator" reads as "no". A wrong answer that is indistinguishable
// from a real one is worth more tests than a wrong answer that panics.

func node(t *testing.T, h http.HandlerFunc) *Node {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Node{RPC: srv.URL, HTTP: srv.Client()}
}

// reply builds the shape a modern node sends: payload at response.Data, base64.
func reply(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"Data":%q,"Error":null,"Log":""}}}`,
		base64.StdEncoding.EncodeToString([]byte(data)))
}

// nestedReply builds the shape older nodes send: the same three fields one level
// down, under ResponseBase. Both are live in the wild — kourt.xyz answers this way.
func nestedReply(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{"Data":%q,"Error":null,"Log":""}}}}`,
		base64.StdEncoding.EncodeToString([]byte(data)))
}

func TestTheExpressionIsSentBase64UnderTheRealmPath(t *testing.T) {
	var got struct {
		Params struct {
			Path string `json:"path"`
			Data string `json:"data"`
		} `json:"params"`
		Method string `json:"method"`
		ID     string `json:"id"`
	}
	n := node(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		reply(w, "(true bool)")
	})
	n.ID = "test-caller"

	if _, err := n.QEval(context.Background(), "gno.land/r/kourt/kourtv2", `IsCourtMod("meta","g1abc")`); err != nil {
		t.Fatal(err)
	}
	if got.Method != "abci_query" || got.Params.Path != "vm/qeval" {
		t.Errorf("method/path = %q/%q", got.Method, got.Params.Path)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Params.Data)
	if err != nil {
		t.Fatalf("data was not base64: %v", err)
	}
	want := `gno.land/r/kourt/kourtv2.IsCourtMod("meta","g1abc")`
	if string(raw) != want {
		t.Errorf("sent %q, want %q", raw, want)
	}
	if got.ID != "test-caller" {
		t.Errorf("id = %q; the caller's name is what makes a noisy reader identifiable "+
			"in a node log", got.ID)
	}
}

func TestBothReplyShapesAreUnderstood(t *testing.T) {
	for _, c := range []struct {
		name string
		send func(http.ResponseWriter, string)
	}{
		{"modern, payload at response.Data", reply},
		{"older, payload under ResponseBase", nestedReply},
	} {
		n := node(t, func(w http.ResponseWriter, r *http.Request) { c.send(w, "(true bool)") })
		got, err := n.QEval(context.Background(), "p", "F()")
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != "(true bool)" {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
}

// A QUERY ERROR IS NOT A TRANSPORT ERROR and must not read as an empty answer.
// This is the case that matters most: "no such court" arriving as "" would make
// a nonexistent court look like one that simply said no.
func TestAQueryErrorIsAnError(t *testing.T) {
	n := node(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{`+
			`"Error":{"@type":"/abci.StringError","value":"kourtv2: no such court"},`+
			`"Log":"kourtv2: no such court","Data":null}}}}`)
	})
	got, err := n.QEval(context.Background(), "p", `IsCourtMod("nope","g1")`)
	if err == nil {
		t.Fatalf("a realm panic came back as a clean %q", got)
	}
	if !strings.Contains(err.Error(), "no such court") {
		t.Errorf("the error loses what the chain said: %v", err)
	}
}

func TestATransportErrorIsAnError(t *testing.T) {
	n := node(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","error":{"message":"Invalid params"}}`)
	})
	if _, err := n.QEval(context.Background(), "p", "F()"); err == nil {
		t.Error("a JSON-RPC error came back clean")
	}

	n2 := node(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := n2.QEval(context.Background(), "p", "F()"); err == nil {
		t.Error("a 502 came back clean")
	}
}

func TestUndecodableDataIsAnErrorRatherThanSilence(t *testing.T) {
	n := node(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"Data":"!!!not base64!!!"}}}`)
	})
	if _, err := n.QEval(context.Background(), "p", "F()"); err == nil {
		t.Error("a corrupt payload was reported as a successful empty answer")
	}
}

// An empty Data with no error is a real, distinct case — the node answered and
// there was nothing to say. It must not be conflated with a failure, and a caller
// that cares has to be able to tell.
func TestAnEmptyAnswerIsNotAnError(t *testing.T) {
	n := node(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"Data":"","Error":null}}}`)
	})
	got, err := n.QEval(context.Background(), "p", "F()")
	if err != nil {
		t.Fatalf("an empty answer was an error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAContextCancellationStopsTheQuery(t *testing.T) {
	n := node(t, func(w http.ResponseWriter, r *http.Request) { reply(w, "(1 int)") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := n.QEval(ctx, "p", "F()"); err == nil {
		t.Error("a cancelled context still produced an answer")
	}
}
