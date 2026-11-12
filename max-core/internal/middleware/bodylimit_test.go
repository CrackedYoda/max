package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/shared"
)

func TestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(1024))
	r.POST("/", func(c *gin.Context) {
		var body struct {
			Value string `json:"value"`
		}
		if err := shared.BindJSON(c, &body); err != nil {
			shared.Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})

	do := func(body io.Reader, contentLength int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLength
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	small := `{"value":"ok"}`
	big := `{"value":"` + strings.Repeat("a", 8<<20) + `"}`

	if w := do(strings.NewReader(small), int64(len(small))); w.Code != http.StatusNoContent {
		t.Fatalf("small body: %d %s", w.Code, w.Body)
	}
	if w := do(strings.NewReader(big), int64(len(big))); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared large body: %d", w.Code)
	}
	// Chunked: no Content-Length, so the reader must stop it.
	if w := do(strings.NewReader(big), -1); w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "BODY_TOO_LARGE") {
		t.Fatalf("chunked large body: %d %s", w.Code, w.Body)
	}
}
