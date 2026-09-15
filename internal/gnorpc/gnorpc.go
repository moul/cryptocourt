// Package gnorpc is the transport for reading a gno realm over JSON-RPC.
//
// IT IS THE TRANSPORT AND NOTHING ELSE. Callers ask for an expression and get
// back the string qeval printed; deciding what `(true bool)` means is the
// caller's job, because the alternative is one package that knows about every
// realm function anybody reads.
//
// It exists because this was written twice. internal/archive had it first, for
// one question — "does claim N reference this hash?" — and its comment argued,
// correctly at the time, that a general node client would be a larger surface
// than that job needed. The Discord bridge then needed the same envelope for a
// different question, and the envelope is not the trivial part: the reply nests
// its payload under ResponseBase on some nodes and not others, the payload is
// base64, and a query error arrives in a different field from a transport error.
// Getting any of those wrong yields an empty string rather than a failure, which
// reads as "the chain said no".
package gnorpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxReplyBytes caps what is read from a node. A qeval answer is a line or two;
// four megabytes is far above any real one and far below a memory problem.
const maxReplyBytes = 4 << 20

// Node is a gno node's JSON-RPC endpoint.
type Node struct {
	// RPC is the endpoint, e.g. https://rpc.kourt.xyz.
	RPC string
	// HTTP is the client used for queries; nil means a 10-second default.
	HTTP *http.Client
	// ID is the jsonrpc id sent with each request. It is echoed in node logs, so
	// naming the caller here is what makes a noisy reader identifiable later.
	ID string
}

type rpcResponse struct {
	Error  *struct{ Message string } `json:"error"`
	Result struct {
		Response struct {
			Data         string `json:"Data"`
			Error        any    `json:"Error"`
			Log          string `json:"Log"`
			ResponseBase *struct {
				Data  string `json:"Data"`
				Error any    `json:"Error"`
				Log   string `json:"Log"`
			} `json:"ResponseBase"`
		} `json:"response"`
	} `json:"result"`
}

// QEval evaluates `pkgPath.expr` and returns what qeval printed.
//
// An empty string with a nil error means the node answered with no data, which
// is not the same as an error and not the same as `("" string)`.
func (n *Node) QEval(ctx context.Context, pkgPath, expr string) (string, error) {
	id := n.ID
	if id == "" {
		id = "gnorpc"
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "abci_query",
		"params": map[string]any{
			"path":   "vm/qeval",
			"data":   base64.StdEncoding.EncodeToString([]byte(pkgPath + "." + expr)),
			"height": "0", "prove": false,
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.RPC, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := n.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("node HTTP %d", res.StatusCode)
	}

	// Bounded, for the reason discordapi's cap gives: a node is trusted by
	// configuration, and a misdirected RPC endpoint should cost a bounded read.
	var out rpcResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxReplyBytes)).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("rpc: %s", out.Error.Message)
	}
	r := out.Result.Response
	data, logMsg, qErr := r.Data, r.Log, r.Error
	if r.ResponseBase != nil {
		// Older nodes nest the same three fields one level down.
		if data == "" {
			data = r.ResponseBase.Data
		}
		if qErr == nil {
			qErr, logMsg = r.ResponseBase.Error, r.ResponseBase.Log
		}
	}
	if qErr != nil {
		return "", fmt.Errorf("query failed: %s", logMsg)
	}
	if data == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("decoding node reply: %w", err)
	}
	return string(raw), nil
}
