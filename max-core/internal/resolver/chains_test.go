package resolver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func listedChains(t *testing.T, h *Handler) (keys []string, testnets int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h.Chains(c)
	var body struct {
		Data []struct {
			Key     string `json:"key"`
			Testnet bool   `json:"testnet"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Data {
		keys = append(keys, c.Key)
		if c.Testnet {
			testnets++
		}
	}
	return keys, testnets
}

func TestChains_HidesTestnetsByDefault(t *testing.T) {
	keys, testnets := listedChains(t, NewHandler(nil))
	if testnets != 0 || len(keys) == 0 {
		t.Fatalf("default list = %v (%d testnets)", keys, testnets)
	}
	if _, testnets := listedChains(t, NewHandler(nil).WithTestnets(true)); testnets == 0 {
		t.Fatal("TESTNETS_ENABLED should list testnets")
	}
}
