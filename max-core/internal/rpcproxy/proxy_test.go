package rpcproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setup(t *testing.T) (*gin.Engine, *[]string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`))
	}))
	t.Cleanup(upstream.Close)
	r := gin.New()
	r.POST("/v1/rpc/:chain", NewHandler(map[string]string{"solana": upstream.URL, "base": upstream.URL}).Forward)
	return r, &seen
}

func post(r *gin.Engine, chain, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/rpc/"+chain, strings.NewReader(body)))
	return rec
}

func TestForward(t *testing.T) {
	r, seen := setup(t)
	tests := []struct {
		name, chain, body string
		code              int
		forwarded         bool
	}{
		{"solana blockhash", "solana", `{"jsonrpc":"2.0","id":1,"method":"getLatestBlockhash","params":[]}`, 200, true},
		{"solana broadcast", "solana", `{"jsonrpc":"2.0","id":1,"method":"sendTransaction","params":["AQ=="]}`, 200, true},
		{"evm balance", "base", `{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x0","latest"]}`, 200, true},
		{"batch of allowed calls", "base", `[{"jsonrpc":"2.0","id":1,"method":"eth_chainId"},{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}]`, 200, true},
		{"evm method on solana", "solana", `{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":[]}`, 403, false},
		{"heavy solana scan refused", "solana", `{"jsonrpc":"2.0","id":1,"method":"getProgramAccounts","params":[]}`, 403, false},
		{"signing method refused", "base", `{"jsonrpc":"2.0","id":1,"method":"eth_sendTransaction","params":[]}`, 403, false},
		{"one bad call fails the batch", "base", `[{"jsonrpc":"2.0","id":1,"method":"eth_chainId"},{"jsonrpc":"2.0","id":2,"method":"debug_traceTransaction"}]`, 403, false},
		{"chain without an RPC", "ethereum", `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`, 404, false},
		{"unknown chain", "nowhere", `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`, 404, false},
		{"not JSON", "base", `hello`, 400, false},
		{"empty batch", "base", `[]`, 400, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(*seen)
			rec := post(r, tt.chain, tt.body)
			if rec.Code != tt.code {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tt.code, rec.Body)
			}
			if got := len(*seen) > before; got != tt.forwarded {
				t.Fatalf("forwarded = %v, want %v", got, tt.forwarded)
			}
			if tt.forwarded && (*seen)[len(*seen)-1] != tt.body {
				t.Errorf("upstream got %q, want the body unchanged", (*seen)[len(*seen)-1])
			}
		})
	}
}

func TestForward_BatchLimit(t *testing.T) {
	r, _ := setup(t)
	calls := make([]string, maxBatch+1)
	for i := range calls {
		calls[i] = `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`
	}
	if rec := post(r, "base", "["+strings.Join(calls, ",")+"]"); rec.Code != 400 {
		t.Fatalf("oversized batch: %d", rec.Code)
	}
}
