package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	mcpPath           = "/mcp"
	defaultListen     = "127.0.0.1:8765"
	defaultMCPVersion = "2025-03-26"
	maxMCPBody        = 1 << 20
)

var mcpVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

// listenAndServe runs the Streamable HTTP MCP endpoint.
// A non-loopback address requires a bearer token so the position feed is not open on the network.
func listenAndServe(addr, token string) error {
	if err := validateListen(addr, token); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mcpHandler(token),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	fmt.Fprintf(osStderr, "mumblelink MCP on http://%s%s\n", addr, mcpPath)
	if token != "" {
		fmt.Fprintln(osStderr, "authorization: Bearer token required")
	}
	return srv.ListenAndServe()
}

// osStderr is set so tests can swap it. The real value is os.Stderr.
var osStderr io.Writer = stdErr

func validateListen(addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("mumblelink: listen address: %w", err)
	}
	if token == "" && !isLoopback(host) {
		return fmt.Errorf("mumblelink: refusing to listen on %s without GW2ML_TOKEN", addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func mcpHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc(mcpPath, func(w http.ResponseWriter, r *http.Request) {
		serveMCPHTTP(w, r, token)
	})
	return mux
}

func serveMCPHTTP(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, MCP-Protocol-Version, Mcp-Session-Id, Last-Event-ID")
	w.Header().Set("Access-Control-Expose-Headers", "MCP-Protocol-Version, Mcp-Session-Id")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, rpcErr(nil, -32000, "this server speaks Streamable HTTP: POST "+mcpPath))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !originAllowed(r.Header.Get("Origin")) {
		writeJSON(w, http.StatusForbidden, rpcErr(nil, -32000, "origin not allowed"))
		return
	}
	if !authorized(r, token) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, rpcErr(nil, -32000, "unauthorized"))
		return
	}
	if pv := r.Header.Get("MCP-Protocol-Version"); pv != "" && !mcpVersions[pv] {
		writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32000, "unsupported MCP-Protocol-Version"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBody+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32700, "read body"))
		return
	}
	if len(body) > maxMCPBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, rpcErr(nil, -32000, "body too large"))
		return
	}
	body = bytesTrimSpace(body)
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32700, "empty body"))
		return
	}
	if body[0] == '[' {
		var reqs []rpcReq
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32700, "parse error"))
			return
		}
		var out []rpcResp
		for _, req := range reqs {
			resp, ok := dispatch(req)
			if ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32700, "parse error"))
		return
	}
	resp, ok := dispatch(req)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	got := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(got, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(token)) == 1
}

func originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") || strings.HasPrefix(origin, "http://[::1]") {
		return true
	}
	for _, allowed := range strings.Split(envOr("GW2ML_ORIGIN", ""), ",") {
		if allowed != "" && origin == allowed {
			return true
		}
	}
	return false
}

func negotiateVersion(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &p) == nil && mcpVersions[p.ProtocolVersion] {
		return p.ProtocolVersion
	}
	return defaultMCPVersion
}

func dispatch(req rpcReq) (rpcResp, bool) {
	if req.ID == nil {
		return rpcResp{}, false
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": negotiateVersion(req.Params),
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gw2-mumblelink", "version": "0.2.0"},
			"instructions":    "Read-only Guild Wars 2 position from the machine running this server. get_link returns one MumbleLink frame. where_am_i names the map and the nearest landmarks, with a straight-line distance and a left or right turn. The game must be in the world on that machine.",
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": []any{getLinkTool(), whereTool()}}
	case "tools/call":
		resp.Result = callTool(req.Params)
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
	}
	return resp, true
}

func rpcErr(id any, code int, msg string) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Error: map[string]any{"code": code, "message": msg}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
