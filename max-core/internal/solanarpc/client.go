package solanarpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

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

// AccountKey is a jsonParsed message account key.
type AccountKey struct {
	Pubkey   string `json:"pubkey"`
	Signer   bool   `json:"signer"`
	Writable bool   `json:"writable"`
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
			// AccountKeys[0] is the fee payer.
			AccountKeys  []AccountKey        `json:"accountKeys"`
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

// FeePayer returns the first account key, or "" when the transaction has none.
func (t *Transaction) FeePayer() string {
	if len(t.Transaction.Message.AccountKeys) == 0 {
		return ""
	}
	return t.Transaction.Message.AccountKeys[0].Pubkey
}

// Balance returns the lamport balance of addr at confirmed commitment.
func (c *Client) Balance(ctx context.Context, addr string) (uint64, error) {
	var out struct {
		Value uint64 `json:"value"`
	}
	cfg := map[string]any{"commitment": "confirmed"}
	if _, err := c.rpc.Call(ctx, "getBalance", []any{addr, cfg}, &out); err != nil {
		return 0, err
	}
	return out.Value, nil
}

// TokenAccountsByOwner returns the total base units of mint held across all
// token accounts of owner.
func (c *Client) TokenAccountsByOwner(ctx context.Context, owner, mint string) (uint64, error) {
	var out struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount string `json:"amount"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	cfg := map[string]any{"encoding": "jsonParsed", "commitment": "confirmed"}
	if _, err := c.rpc.Call(ctx, "getTokenAccountsByOwner", []any{owner, map[string]any{"mint": mint}, cfg}, &out); err != nil {
		return 0, err
	}
	total := new(big.Int)
	for _, v := range out.Value {
		n, ok := new(big.Int).SetString(v.Account.Data.Parsed.Info.TokenAmount.Amount, 10)
		if !ok {
			return 0, fmt.Errorf("solanarpc: bad token amount %q", v.Account.Data.Parsed.Info.TokenAmount.Amount)
		}
		total.Add(total, n)
	}
	if !total.IsUint64() {
		return 0, fmt.Errorf("solanarpc: token balance overflows uint64")
	}
	return total.Uint64(), nil
}

// MinimumBalanceForRentExemption returns the lamports needed to keep an
// account of the given data size rent-exempt.
func (c *Client) MinimumBalanceForRentExemption(ctx context.Context, size int) (uint64, error) {
	var out uint64
	if _, err := c.rpc.Call(ctx, "getMinimumBalanceForRentExemption", []any{size}, &out); err != nil {
		return 0, err
	}
	return out, nil
}
