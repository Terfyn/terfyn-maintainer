// Command terfyn-git-publish is the MCP-over-stdio tool that backs the `git`
// tool in the terfyn-maintainer project (DESIGN.md §9). It speaks newline-
// delimited JSON-RPC 2.0 — the same wire format as Terfyn's stdio MCP client —
// and exposes exactly two operations: create_branch and push_branch.
//
// The checkout it operates on is TERFYN_WORKSPACE_ROOT (the same sandbox the
// native workspace tool reads and writes); the push remote is TERFYN_GIT_REMOTE
// (default "origin").
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Terfyn/terfyn-maintainer/internal/gitpublish"
)

const protocolVersion = "2024-11-05"

func main() {
	root := os.Getenv("TERFYN_WORKSPACE_ROOT")
	if root == "" {
		root = "."
	}
	pub, err := gitpublish.New(root, os.Getenv("TERFYN_GIT_REMOTE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "terfyn-git-publish:", err)
		os.Exit(1)
	}
	if err := serve(context.Background(), pub, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "terfyn-git-publish:", err)
		os.Exit(1)
	}
}

// operation runs one tools/call operation.
type operation func(ctx context.Context, args map[string]any) (map[string]any, error)

func serve(ctx context.Context, pub *gitpublish.Publisher, in *os.File, out *os.File) error {
	ops := map[string]operation{
		"create_branch": func(ctx context.Context, a map[string]any) (map[string]any, error) {
			return pub.CreateBranch(ctx, stringArg(a, "name"))
		},
		"push_branch": func(ctx context.Context, a map[string]any) (map[string]any, error) {
			return pub.PushBranch(ctx, stringArg(a, "branch"))
		},
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)

	for sc.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue // ignore malformed lines
		}
		method, _ := msg["method"].(string)
		id, hasID := msg["id"]
		if !hasID || msg["id"] == nil {
			continue // a notification (e.g. notifications/initialized) — no reply
		}

		switch method {
		case "initialize":
			writeResult(enc, id, map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "terfyn-git-publish", "version": "1"},
			})
		case "tools/list":
			writeResult(enc, id, map[string]any{"tools": toolManifest()})
		case "tools/call":
			name, args := callParams(msg)
			op, ok := ops[name]
			if !ok {
				writeToolError(enc, id, fmt.Sprintf("unknown operation %q", name))
				continue
			}
			result, err := op(ctx, args)
			if err != nil {
				writeToolError(enc, id, err.Error())
				continue
			}
			writeToolText(enc, id, result)
		default:
			writeError(enc, id, -32601, "method not found")
		}
	}
	return sc.Err()
}

func toolManifest() []any {
	sideEffecting := map[string]any{"mcp_flags": map[string]any{
		"trusted": true, "side_effects": true,
	}}
	return []any{
		map[string]any{
			"name":        "create_branch",
			"description": "Create and switch to a new local branch.",
			"meta":        sideEffecting,
		},
		map[string]any{
			"name":        "push_branch",
			"description": "Push a branch to the configured remote (the publication boundary).",
			"meta":        sideEffecting,
		},
	}
}

func callParams(msg map[string]any) (name string, args map[string]any) {
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		return "", map[string]any{}
	}
	name, _ = params["name"].(string)
	if a, ok := params["arguments"].(map[string]any); ok {
		return name, a
	}
	return name, map[string]any{}
}

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func writeResult(enc *json.Encoder, id any, result map[string]any) {
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeToolText(enc *json.Encoder, id any, obj map[string]any) {
	b, _ := json.Marshal(obj)
	writeResult(enc, id, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(b)}},
	})
}

func writeToolError(enc *json.Encoder, id any, message string) {
	writeResult(enc, id, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": message}},
		"isError": true,
	})
}

func writeError(enc *json.Encoder, id any, code int, message string) {
	_ = enc.Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
