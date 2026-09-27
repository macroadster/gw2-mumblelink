// Command gw2-mumblelink is a read-only PoC.
//
//	gw2-mumblelink read            read the live block from the running game
//	gw2-mumblelink where           map name, nearest landmarks, and which way to turn
//	gw2-mumblelink dump <file>     decode one copied MumbleLink block
//	gw2-mumblelink mcp             stdio MCP server (tools: get_link, where_am_i)
//	gw2-mumblelink serve           Streamable HTTP MCP on this machine, for a remote client
//
// On macOS, read launches the Windows half of this command inside the Wine
// prefix that is running Gw2-64.exe and opens the MumbleLink file mapping.
// It never opens Gw2-64.exe. processId in the block is only a label.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/macroadster/gw2-mumblelink"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "read":
		fs := flag.NewFlagSet("read", flag.ContinueOnError)
		wait := fs.Duration("wait", 15*time.Second, "how long to wait for Guild Wars 2 to write a frame")
		name := fs.String("name", envOr("MUMBLELINK_NAME", "MumbleLink"), "Win32 mapping name")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if err := dumpSnapshot(os.Stdout, *name, *wait); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "where":
		fs := flag.NewFlagSet("where", flag.ContinueOnError)
		wait := fs.Duration("wait", 15*time.Second, "how long to wait for Guild Wars 2 to write a frame")
		name := fs.String("name", envOr("MUMBLELINK_NAME", "MumbleLink"), "Win32 mapping name")
		limit := fs.Int("limit", 8, "how many nearby landmarks to return")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		s, err := readSnapshot(*name, *wait)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		place, err := mumblelink.NewClient().Where(context.Background(), s, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(place); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "raw":
		if len(os.Args) != 5 {
			fmt.Fprintln(os.Stderr, "usage: gw2-mumblelink raw <outfile> <name> <waitms>")
			os.Exit(2)
		}
		ms, err := strconv.Atoi(os.Args[4])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := mumblelink.WriteMappingFile(os.Args[2], os.Args[3], time.Duration(ms)*time.Millisecond); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "dump":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		s, err := mumblelink.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mcp":
		if err := serveMCP(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		listen := fs.String("listen", envOr("GW2ML_LISTEN", defaultListen), "host:port for the Streamable HTTP MCP endpoint")
		token := fs.String("token", os.Getenv("GW2ML_TOKEN"), "bearer token; required when listen is not loopback")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if err := listenAndServe(*listen, *token); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gw2-mumblelink read [-wait 15s] [-name MumbleLink] | where [-limit 8] | dump <file> | mcp | serve [-listen host:port]")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readSnapshot(name string, wait time.Duration) (mumblelink.Snapshot, error) {
	b, err := mumblelink.ReadShared(name, wait)
	if err != nil {
		return mumblelink.Snapshot{}, err
	}
	return mumblelink.Decode(b)
}

func dumpSnapshot(w io.Writer, name string, wait time.Duration) error {
	s, err := readSnapshot(name, wait)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func serveMCP(in io.Reader, out io.Writer) error {
	for {
		body, err := readFrame(in)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var req rpcReq
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification
		}
		resp, ok := dispatch(req)
		if !ok {
			continue
		}
		if err := writeFrame(out, resp); err != nil {
			return err
		}
	}
}

func getLinkTool() map[string]any {
	return map[string]any{
		"name":        "get_link",
		"description": "Read one Guild Wars 2 MumbleLink snapshot. Pass path or set MUMBLELINK_PATH to decode a file. With neither, read the live mapping from Guild Wars 2 (on macOS, via the Wine prefix that is running the game). Does not open Gw2-64.exe.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to a 5460-byte MumbleLink dump. Defaults to MUMBLELINK_PATH.",
				},
			},
		},
	}
}

func whereTool() map[string]any {
	return map[string]any{
		"name":        "where_am_i",
		"description": "Read the live Guild Wars 2 position and name the map, the nearest waypoint, and the nearest landmarks. Each landmark includes straight-line distance and whether to turn left or right. Does not path around terrain. Does not open Gw2-64.exe.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Optional 5460-byte MumbleLink dump. Defaults to a live read, or MUMBLELINK_PATH.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "How many nearby landmarks to return. Default 8.",
				},
			},
		},
	}
}

func callTool(params json.RawMessage) map[string]any {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Name != "get_link" && p.Name != "where_am_i" {
		return toolErr("unknown tool: " + p.Name)
	}
	var args struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	path := args.Path
	if path == "" {
		path = os.Getenv("MUMBLELINK_PATH")
	}
	var s mumblelink.Snapshot
	var err error
	if path == "" {
		s, err = readSnapshot(envOr("MUMBLELINK_NAME", "MumbleLink"), 15*time.Second)
	} else {
		s, err = mumblelink.ReadFile(path)
	}
	if err != nil {
		return toolErr(err.Error())
	}
	if p.Name == "where_am_i" {
		place, err := mumblelink.NewClient().Where(context.Background(), s, args.Limit)
		if err != nil {
			return toolErr(err.Error())
		}
		b, err := marshalText(place)
		if err != nil {
			return toolErr(err.Error())
		}
		return map[string]any{
			"content": []any{map[string]any{"type": "text", "text": string(b)}},
		}
	}
	b, err := marshalText(s)
	if err != nil {
		return toolErr(err.Error())
	}
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(b)}},
	}
}

func marshalText(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toolErr(msg string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	}
}

func readFrame(r io.Reader) ([]byte, error) {
	// LSP-style Content-Length framing, which MCP stdio uses.
	var n int
	for {
		var line [1]byte
		buf := make([]byte, 0, 64)
		for {
			_, err := r.Read(line[:])
			if err != nil {
				return nil, err
			}
			if line[0] == '\n' {
				break
			}
			if line[0] != '\r' {
				buf = append(buf, line[0])
			}
		}
		s := string(buf)
		if s == "" {
			break
		}
		if len(s) >= 16 && s[:15] == "Content-Length:" {
			fmt.Sscanf(s[15:], "%d", &n)
		}
	}
	if n <= 0 || n > 1<<20 {
		return nil, fmt.Errorf("bad content-length %d", n)
	}
	body := make([]byte, n)
	_, err := io.ReadFull(r, body)
	return body, err
}

func writeFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
