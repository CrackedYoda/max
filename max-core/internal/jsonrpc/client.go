package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Client is a minimal JSON-RPC 2.0 client shared by the EVM and Solana adapters.
type Client struct {
	url  string
	http *http.Client
	id   atomic.Int64
}

func New(url string) *Client {
	return &Client{url: url, http: &http.Client{Timeout: 10 * time.Second}}
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Call invokes method and decodes the result into out. A JSON null result
// leaves out untouched and returns (false, nil).
func (c *Client) Call(ctx context.Context, method string, params []any, out any) (bool, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(request{JSONRPC: "2.0", ID: c.id.Add(1), Method: method, Params: params})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s: http %d", method, resp.StatusCode)
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return false, fmt.Errorf("%s: decode: %w", method, err)
	}
	if r.Error != nil {
		return false, fmt.Errorf("%s: %w", method, r.Error)
	}
	if len(r.Result) == 0 || string(r.Result) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return false, fmt.Errorf("%s: decode result: %w", method, err)
	}
	return true, nil
}
