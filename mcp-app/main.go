// Command idd-mcp is the In Darkened Dreams MCP App server. It speaks MCP over
// stdio (line-delimited JSON-RPC 2.0) and exposes character-building tools plus
// an interactive UI resource (an MCP App) that renders in Claude Desktop.
//
// The server only speaks plain MCP — tools + a ui:// resource. The interactive
// ui/ postMessage bridge is between the applet HTML and the host, not this server.
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/verveguy/idd/mcp-app/rules"
)

const (
	protocolVersion = "2026-01-26"
	uiResourceURI   = "ui://idd/builder"
	uiMimeType      = "text/html;profile=mcp-app"
)

//go:embed ui/builder.html
var builderHTML string

var ruleset *rules.Ruleset

// ---------- JSON-RPC ----------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func main() {
	rs, err := rules.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal: load ruleset:", err)
		os.Exit(1)
	}
	ruleset = rs

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue // ignore unparseable lines
		}
		// Notifications (no id) get no response.
		isNotification := len(req.ID) == 0
		result, rerr := dispatch(req)
		if isNotification {
			continue
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp)
		out.Flush()
	}
}

func dispatch(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo":      map[string]any{"name": "in-darkened-dreams", "version": "1.0.0"},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "resources/list":
		return map[string]any{"resources": []any{
			map[string]any{"uri": uiResourceURI, "name": "IDD Character Builder", "mimeType": uiMimeType},
		}}, nil
	case "resources/read":
		return readResource(req.Params)
	case "tools/call":
		return callTool(req.Params)
	case "notifications/initialized", "notifications/cancelled":
		return map[string]any{}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

// ---------- tools ----------

func toolDefs() []any {
	uiMeta := map[string]any{
		"ui": map[string]any{
			"resourceUri": uiResourceURI,
			"visibility":  []string{"model", "app"},
		},
		"ui/resourceUri": uiResourceURI, // legacy compat
	}
	buildObj := map[string]any{"type": "object", "description": "An IDD character build object."}
	return []any{
		map[string]any{
			"name":        "open_character_builder",
			"description": "Open the interactive In Darkened Dreams character builder UI, optionally seeded with a build.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}},
			"_meta":       uiMeta,
		},
		map[string]any{
			"name":        "validate_character",
			"description": "Validate an IDD character build against the rules (CP, attributes, headers, skills, faction, prerequisites, exclusions). Returns errors, warnings, CP breakdown, Vitality, tier, and traits.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
		},
		map[string]any{
			"name":        "get_catalog",
			"description": "List the IDD ruleset options: heritages, factions, and all headers with their CP cost and faction requirement.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "save_character",
			"description": "Save an IDD character build to local storage on this machine, by name.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
		},
		map[string]any{
			"name":        "load_character",
			"description": "Load a saved IDD character build from local storage by name.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		},
		map[string]any{
			"name":        "list_characters",
			"description": "List the names of IDD character builds saved on this machine.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	switch p.Name {
	case "validate_character":
		return toolValidate(p.Arguments)
	case "get_catalog":
		return toolCatalog()
	case "open_character_builder":
		return toolOpenBuilder(p.Arguments)
	case "save_character":
		return toolSave(p.Arguments)
	case "load_character":
		return toolLoad(p.Arguments)
	case "list_characters":
		return toolListCharacters()
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
}

func textResult(text string, structured any) map[string]any {
	m := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
	if structured != nil {
		m["structuredContent"] = structured
	}
	return m
}

func toolValidate(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Build rules.Build `json:"build"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid build: " + err.Error()}
	}
	res := ruleset.Validate(a.Build)
	var summary string
	if res.Valid {
		summary = fmt.Sprintf("VALID — %d/%d CP spent, %d remaining. Vitality %d, %s.",
			res.Total, res.Available, res.Remaining, res.Vitality, res.Tier)
	} else {
		summary = fmt.Sprintf("INVALID — %d error(s): %s", len(res.Errors), strings.Join(res.Errors, " | "))
	}
	return textResult(summary, res), nil
}

func toolCatalog() (any, *rpcError) {
	type hdr struct {
		Name    string `json:"name"`
		Cost    int    `json:"cost"`
		Faction string `json:"faction,omitempty"`
	}
	var headers []hdr
	for i := range ruleset.Headers {
		h := &ruleset.Headers[i]
		fac := ""
		for _, p := range h.Prerequisites {
			if m := regexp.MustCompile(`(?i)member of (the [\w' ]+?)(?:\.|$| to)`).FindStringSubmatch(p); m != nil {
				fac = strings.TrimSpace(m[1])
			}
		}
		headers = append(headers, hdr{h.Name, h.HeaderCost, fac})
	}
	var heritages, factions []string
	for _, h := range ruleset.Heritages {
		heritages = append(heritages, h.Name)
	}
	for _, f := range ruleset.Factions {
		factions = append(factions, f.Name)
	}
	cat := map[string]any{"heritages": heritages, "factions": factions, "headers": headers}
	return textResult(fmt.Sprintf("%d heritages, %d factions, %d headers.", len(heritages), len(factions), len(headers)), cat), nil
}

func toolOpenBuilder(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Build json.RawMessage `json:"build"`
	}
	_ = json.Unmarshal(args, &a)
	structured := map[string]any{}
	if len(a.Build) > 0 {
		var b any
		if json.Unmarshal(a.Build, &b) == nil {
			structured["build"] = b
		}
	}
	return textResult("Opening the In Darkened Dreams character builder.", structured), nil
}

// ---------- persistence ----------

func storeDir() (string, error) {
	if d := os.Getenv("IDD_DATA_DIR"); d != "" {
		return d, os.MkdirAll(d, 0o755)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".idd", "characters")
	return d, os.MkdirAll(d, 0o755)
}

var reSafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeFile(name string) string {
	s := reSafeName.ReplaceAllString(strings.TrimSpace(name), "-")
	if s == "" {
		s = "unnamed"
	}
	return s + ".json"
}

func toolSave(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Build json.RawMessage `json:"build"`
	}
	if err := json.Unmarshal(args, &a); err != nil || len(a.Build) == 0 {
		return nil, &rpcError{Code: -32602, Message: "missing build"}
	}
	var nameHolder struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(a.Build, &nameHolder)
	dir, err := storeDir()
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	path := filepath.Join(dir, safeFile(nameHolder.Name))
	if err := os.WriteFile(path, a.Build, 0o644); err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return textResult(fmt.Sprintf("Saved %q to %s.", nameHolder.Name, path), map[string]any{"path": path}), nil
}

func toolLoad(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	dir, err := storeDir()
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	raw, err := os.ReadFile(filepath.Join(dir, safeFile(a.Name)))
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: "not found: " + a.Name}
	}
	var b any
	_ = json.Unmarshal(raw, &b)
	return textResult(fmt.Sprintf("Loaded %q.", a.Name), map[string]any{"build": b}), nil
}

func toolListCharacters() (any, *rpcError) {
	dir, err := storeDir()
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return textResult(fmt.Sprintf("%d saved character(s).", len(names)), map[string]any{"names": names}), nil
}

// ---------- ui resource ----------

func readResource(params json.RawMessage) (any, *rpcError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	if p.URI != uiResourceURI {
		return nil, &rpcError{Code: -32002, Message: "resource not found: " + p.URI}
	}
	html := strings.Replace(builderHTML, "/*__IDD_DATA__*/", uiDataJSON(), 1)
	return map[string]any{"contents": []any{
		map[string]any{"uri": uiResourceURI, "mimeType": uiMimeType, "text": html},
	}}, nil
}
