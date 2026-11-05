package evmrpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func rpcServer(t *testing.T, result string, check func(method string, params []any)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if check != nil {
			check(req.Method, req.Params)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(rune('0'+req.ID%10)) + `,"result":` + result + `}`))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestBalance(t *testing.T) {
	c := rpcServer(t, `"0xde0b6b3a7640000"`, func(m string, p []any) {
		if m != "eth_getBalance" || p[0] != "0xabc" || p[1] != "latest" {
			t.Errorf("unexpected call %s %v", m, p)
		}
	})
	v, err := c.Balance(context.Background(), "0xabc")
	if err != nil || v.String() != "1000000000000000000" {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestBalance_NullIsError(t *testing.T) {
	if _, err := rpcServer(t, `null`, nil).Balance(context.Background(), "0xabc"); err == nil {
		t.Fatal("null balance must be an error")
	}
}

func TestERC20Balance(t *testing.T) {
	owner := "0x00000000000000000000000000000000000000Aa"
	c := rpcServer(t, `"0x00000000000000000000000000000000000000000000000000000000000f4240"`, func(m string, p []any) {
		call := p[0].(map[string]any)
		want := "0x70a08231000000000000000000000000" + "00000000000000000000000000000000000000aa"
		if m != "eth_call" || call["to"] != "0xtoken" || call["data"] != want {
			t.Errorf("unexpected call %s %v", m, p)
		}
	})
	v, err := c.ERC20Balance(context.Background(), "0xtoken", owner)
	if err != nil || v.Int64() != 1_000_000 {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestERC20Balance_EmptyIsError(t *testing.T) {
	if _, err := rpcServer(t, `"0x"`, nil).ERC20Balance(context.Background(), "0xtoken", "0xaa"); err == nil {
		t.Fatal("empty result must be an error")
	}
}
