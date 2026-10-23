package xapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPKCEAndAuthorizeURL(t *testing.T) {
	v, ch, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(v))
	if ch != base64.RawURLEncoding.EncodeToString(sum[:]) || len(v) < 43 {
		t.Fatal("challenge is not S256(verifier)")
	}
	c := New(Config{ClientID: "cid", RedirectURI: "http://localhost:8080/v1/verifications/x/callback"})
	u, _ := url.Parse(c.AuthorizeURL("st", ch))
	q := u.Query()
	if u.Host != "x.com" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != Scopes || q.Get("state") != "st" || q.Get("client_id") != "cid" {
		t.Fatalf("authorize url %s", u)
	}
	if strings.Contains(u.RawQuery, "+") {
		t.Fatal("scopes must be %20-separated")
	}
}

func TestExchangeAndMe(t *testing.T) {
	var sawBasic, sawVerifier bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/oauth2/token":
			_ = r.ParseForm()
			_, _, sawBasic = r.BasicAuth()
			sawVerifier = r.PostForm.Get("code_verifier") == "ver" && r.PostForm.Get("grant_type") == "authorization_code"
			w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
		case "/2/users/me":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"data":{"id":"783214","username":"X","name":"X","created_at":"2007-02-20T14:35:54.000Z"}}`))
		case "/2/oauth2/revoke":
			w.Write([]byte(`{"revoked":true}`))
		}
	}))
	defer srv.Close()

	c := New(Config{ClientID: "cid", ClientSecret: "sec", RedirectURI: "http://cb", APIBase: srv.URL})
	tok, err := c.Exchange(context.Background(), "code", "ver")
	if err != nil || tok != "tok" || !sawBasic || !sawVerifier {
		t.Fatalf("exchange: %v %s basic=%v verifier=%v", err, tok, sawBasic, sawVerifier)
	}
	u, err := c.Me(context.Background(), tok)
	if err != nil || u.ID != "783214" || u.Username != "X" || u.CreatedAt == nil {
		t.Fatalf("me: %+v %v", u, err)
	}
	if err := c.Revoke(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Me(context.Background(), "bad"); err == nil {
		t.Fatal("bad token accepted")
	}
}
