package evmrpc

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/jsonrpc"
)

type Client struct {
	rpc *jsonrpc.Client
}

func New(url string) *Client {
	return &Client{rpc: jsonrpc.New(url)}
}

type Log struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	Removed bool     `json:"removed"`
}

type Receipt struct {
	Status      string `json:"status"`
	BlockNumber string `json:"blockNumber"`
	From        string `json:"from"`
	To          string `json:"to"`
	Logs        []Log  `json:"logs"`
}

type Transaction struct {
	Hash        string `json:"hash"`
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	Input       string `json:"input"`
	ChainID     string `json:"chainId"`
	BlockNumber string `json:"blockNumber"`
}

type Block struct {
	Timestamp string `json:"timestamp"`
}

func (c *Client) ChainID(ctx context.Context) (int64, error) {
	var out string
	if _, err := c.rpc.Call(ctx, "eth_chainId", nil, &out); err != nil {
		return 0, err
	}
	return hexInt(out)
}

func (c *Client) BlockNumber(ctx context.Context) (int64, error) {
	var out string
	if _, err := c.rpc.Call(ctx, "eth_blockNumber", nil, &out); err != nil {
		return 0, err
	}
	return hexInt(out)
}

// Receipt returns (nil, nil) while the transaction is pending or unknown.
func (c *Client) Receipt(ctx context.Context, hash string) (*Receipt, error) {
	var r Receipt
	ok, err := c.rpc.Call(ctx, "eth_getTransactionReceipt", []any{hash}, &r)
	if err != nil || !ok {
		return nil, err
	}
	return &r, nil
}

func (c *Client) Transaction(ctx context.Context, hash string) (*Transaction, error) {
	var t Transaction
	ok, err := c.rpc.Call(ctx, "eth_getTransactionByHash", []any{hash}, &t)
	if err != nil || !ok {
		return nil, err
	}
	return &t, nil
}

func (c *Client) BlockTimestamp(ctx context.Context, blockNumberHex string) (int64, error) {
	var b Block
	ok, err := c.rpc.Call(ctx, "eth_getBlockByNumber", []any{blockNumberHex, false}, &b)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("block %s not found", blockNumberHex)
	}
	return hexInt(b.Timestamp)
}

func (c *Client) HasCode(ctx context.Context, addr string) (bool, error) {
	var code string
	if _, err := c.rpc.Call(ctx, "eth_getCode", []any{addr, "latest"}, &code); err != nil {
		return false, err
	}
	return strings.TrimPrefix(code, "0x") != "", nil
}

// ERC20Decimals calls decimals() on a token contract.
func (c *Client) ERC20Decimals(ctx context.Context, token string) (int, error) {
	var out string
	call := map[string]string{"to": token, "data": chains.ERC20DecimalsCallData}
	if _, err := c.rpc.Call(ctx, "eth_call", []any{call, "latest"}, &out); err != nil {
		return 0, err
	}
	v, ok := chains.ParseHexBig(out)
	if !ok || strings.TrimPrefix(out, "0x") == "" || v.Cmp(big.NewInt(36)) > 0 {
		return 0, fmt.Errorf("token did not return a valid decimals() value")
	}
	return int(v.Int64()), nil
}

func hexInt(s string) (int64, error) {
	v, ok := chains.ParseHexBig(s)
	if !ok || !v.IsInt64() {
		return 0, fmt.Errorf("invalid hex quantity %q", s)
	}
	return v.Int64(), nil
}
