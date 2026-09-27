package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateListen(t *testing.T) {
	if err := validateListen("127.0.0.1:8765", ""); err != nil {
		t.Fatal(err)
	}
	if err := validateListen("0.0.0.0:8765", ""); err == nil {
		t.Fatal("expected token required")
	}
	if err := validateListen("0.0.0.0:8765", "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTP(t *testing.T) {
	h := mcpHandler("secret")

	t.Run("unauthorized", func(t *testing.T) {
		rec := postMCP(h, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("initialize", func(t *testing.T) {
		rec := postMCP(h, "secret", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"protocolVersion":"2025-03-26"`) || !strings.Contains(body, "gw2-mumblelink") {
			t.Fatalf("body %s", body)
		}
	})

	t.Run("tools", func(t *testing.T) {
		rec := postMCP(h, "secret", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "get_link") || !strings.Contains(body, "where_am_i") {
			t.Fatalf("status %d body %s", rec.Code, body)
		}
	})

	t.Run("notification", func(t *testing.T) {
		rec := postMCP(h, "secret", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("get link from file", func(t *testing.T) {
		rec := postMCP(h, "secret", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_link","arguments":{"path":"/no/such/MumbleLink"}}}`)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `"isError":true`) {
			t.Fatalf("status %d body %s", rec.Code, body)
		}
	})

	t.Run("origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("get not offered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, mcpPath, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func postMCP(h http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
