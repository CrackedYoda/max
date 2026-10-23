package solanarpc

import (
	"context"
	"encoding/json"

	"github.com/max/max-core/internal/jsonrpc"
)

type Client struct {
	rpc *jsonrpc.Client
}

func New(url string) *Client {
	return &Client{rpc: jsonrpc.New(url)}
}

type ParsedInstruction struct {
	Program   string          `json:"program"`
	ProgramID string          `json:"programId"`
	Parsed    json.RawMessage `json:"parsed"`
}

type InnerInstructions struct {
	Index        int                 `json:"index"`
	Instructions []ParsedInstruction `json:"instructions"`
}

type Transaction struct {
	Slot      uint64 `json:"slot"`
	BlockTime *int64 `json:"blockTime"`
	Meta      *struct {
		Err               json.RawMessage     `json:"err"`
		InnerInstructions []InnerInstructions `json:"innerInstructions"`
	} `json:"meta"`
	Transaction struct {
		Message struct {
			Instructions []ParsedInstruction `json:"instructions"`
		} `json:"message"`
	} `json:"transaction"`
}

// Failed reports whether the transaction executed with an error.
func (t *Transaction) Failed() bool {
	return t.Meta != nil && len(t.Meta.Err) > 0 && string(t.Meta.Err) != "null"
}

// AllInstructions returns top-level and inner (CPI) instructions in order.
func (t *Transaction) AllInstructions() []ParsedInstruction {
	out := append([]ParsedInstruction{}, t.Transaction.Message.Instructions...)
	if t.Meta != nil {
		for _, inner := range t.Meta.InnerInstructions {
			out = append(out, inner.Instructions...)
		}
	}
	return out
}

// Transaction fetches a confirmed transaction with parsed instructions.
// Returns (nil, nil) if it is not (yet) visible at "confirmed" commitment.
func (c *Client) Transaction(ctx context.Context, signature string) (*Transaction, error) {
	var tx Transaction
	cfg := map[string]any{"encoding": "jsonParsed", "commitment": "confirmed", "maxSupportedTransactionVersion": 0}
	ok, err := c.rpc.Call(ctx, "getTransaction", []any{signature, cfg}, &tx)
	if err != nil || !ok {
		return nil, err
	}
	return &tx, nil
}

// ConfirmationStatus returns processed | confirmed | finalized, or "" if unknown.
func (c *Client) ConfirmationStatus(ctx context.Context, signature string) (string, error) {
	var out struct {
		Value []*struct {
			ConfirmationStatus string `json:"confirmationStatus"`
		} `json:"value"`
	}
	cfg := map[string]any{"searchTransactionHistory": true}
	if _, err := c.rpc.Call(ctx, "getSignatureStatuses", []any{[]string{signature}, cfg}, &out); err != nil {
		return "", err
	}
	if len(out.Value) == 0 || out.Value[0] == nil {
		return "", nil
	}
	return out.Value[0].ConfirmationStatus, nil
}

type AccountInfo struct {
	Owner string `json:"owner"`
	Data  struct {
		Parsed *struct {
			Type string          `json:"type"`
			Info json.RawMessage `json:"info"`
		} `json:"parsed"`
	} `json:"data"`
}

// AccountInfo returns (nil, nil) when the account does not exist.
func (c *Client) AccountInfo(ctx context.Context, addr string) (*AccountInfo, error) {
	var out struct {
		Value *AccountInfo `json:"value"`
	}
	cfg := map[string]any{"encoding": "jsonParsed", "commitment": "confirmed"}
	if _, err := c.rpc.Call(ctx, "getAccountInfo", []any{addr, cfg}, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}
