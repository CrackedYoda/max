// Package rpcproxy lets the website and app reach a chain through MAX's own RPC
// endpoints, so provider keys stay on the server. Only the reads and the
// broadcast of already-signed transactions those clients need are forwarded.
package rpcproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

const (
	maxBody  = 64 << 10
	maxBatch = 10
)

// allowed lists the JSON-RPC methods forwarded per namespace. Everything is
// read-only except broadcasting a transaction the user already signed.
var allowed = map[chains.Namespace]map[string]bool{
	chains.NamespaceSolana: set(
		"getLatestBlockhash", "isBlockhashValid", "getBlockHeight", "getSlot",
		"getSignatureStatuses", "getBalance", "getTokenAccountBalance", "getAccountInfo",
		"getFeeForMessage", "getMinimumBalanceForRentExemption", "getRecentPrioritizationFees",
		"sendTransaction",
	),
	chains.NamespaceEVM: set(
		"eth_chainId", "eth_blockNumber", "eth_getBalance", "eth_call", "eth_getTransactionCount",
		"eth_getBlockByNumber", "eth_getTransactionByHash", "eth_getTransactionReceipt",
		"eth_estimateGas", "eth_gasPrice", "eth_maxPriorityFeePerGas", "eth_feeHistory",
		"eth_sendRawTransaction",
	),
}

var (
	ErrUnknownChain = shared.NewAppError("UNSUPPORTED_CHAIN", http.StatusNotFound, "MAX has no RPC for this chain")
	ErrMethod       = shared.NewAppError("RPC_METHOD_NOT_ALLOWED", http.StatusForbidden, "this RPC method isn't available through MAX")
	ErrUpstream     = shared.NewAppError("RPC_UNAVAILABLE", http.StatusBadGateway, "the chain's RPC didn't answer")
)

type Handler struct {
	urls   map[string]string // chain key → upstream RPC URL
	client *http.Client
}

// NewHandler proxies to the RPC URLs MAX is configured with, keyed by chain.
func NewHandler(urls map[string]string) *Handler {
	return &Handler{urls: urls, client: &http.Client{Timeout: 20 * time.Second}}
}

// Forward handles POST /v1/rpc/:chain with a JSON-RPC request or a small batch.
func (h *Handler) Forward(c *gin.Context) {
	chain, ok := chains.Lookup(c.Param("chain"))
	url := h.urls[chain.Key]
	if !ok || url == "" || allowed[chain.Namespace] == nil {
		shared.Fail(c, ErrUnknownChain)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		shared.Fail(c, shared.ErrBadRequest.WithMessage("RPC request is too large"))
		return
	}
	if err := check(body, allowed[chain.Namespace]); err != nil {
		shared.Fail(c, err)
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		shared.Fail(c, ErrUpstream.WithError(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client.Do(req)
	if err != nil {
		shared.Fail(c, ErrUpstream.WithError(err))
		return
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		shared.Fail(c, ErrUpstream.WithError(err))
		return
	}
	c.Data(res.StatusCode, "application/json", out)
}

type call struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// check accepts one JSON-RPC call or a batch of up to maxBatch, every method allowed.
func check(body []byte, methods map[string]bool) error {
	trimmed := bytes.TrimSpace(body)
	var calls []call
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &calls); err != nil {
			return shared.ErrBadRequest.WithMessage("malformed JSON-RPC batch")
		}
		if len(calls) == 0 || len(calls) > maxBatch {
			return shared.ErrBadRequest.WithMessage("batch must hold 1 to 10 calls")
		}
	} else {
		var one call
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return shared.ErrBadRequest.WithMessage("malformed JSON-RPC request")
		}
		calls = []call{one}
	}
	for _, cl := range calls {
		if cl.Method == "" {
			return shared.ErrBadRequest.WithMessage("JSON-RPC method is missing")
		}
		if !methods[cl.Method] {
			return ErrMethod.WithMessage(cl.Method + " isn't available through MAX")
		}
	}
	return nil
}

func set(methods ...string) map[string]bool {
	m := make(map[string]bool, len(methods))
	for _, x := range methods {
		m[x] = true
	}
	return m
}
