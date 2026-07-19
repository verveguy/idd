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
	defaultProtocol = "2025-11-25" // core MCP protocol version (fallback for negotiation)
	uiExtensionName = "io.modelcontextprotocol/ui"
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

	// Opt-in local HTTP head (localhost only): serves the browser builder + a
	// shared session both Claude (MCP) and the browser edit. Runs alongside stdio.
	if addr := httpAddr(); addr != "" {
		go startHTTP(addr)
	}

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
		// Echo the client's core protocol version (negotiation); fall back to a
		// known-good default. NOTE: this is the CORE MCP version (e.g. 2025-11-25),
		// not the MCP Apps extension date (2026-01-26).
		var ip struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &ip)
		pv := ip.ProtocolVersion
		if pv == "" {
			pv = defaultProtocol
		}
		return map[string]any{
			"protocolVersion": pv,
			"serverInfo":      map[string]any{"name": "in-darkened-dreams", "version": "1.0.0"},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				// declare MCP Apps (UI) support so the host will render ui:// resources
				"extensions": map[string]any{
					uiExtensionName: map[string]any{"mimeTypes": []string{uiMimeType}},
				},
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
			"name": "open_character_builder",
			"description": "Open the interactive character-builder panel. Call this AT MOST ONCE per conversation. Once it is open, the panel exposes its own tools — set_heritage, set_faction, set_attribute, add_header/remove_header, add_skill/remove_skill, get_character, validate_build — use THOSE to change or read the live panel (they update the one open panel; they never open a new one). Do NOT call open_character_builder again to show changes. The user can also drive the panel directly and click \"Send to Claude\".",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}},
			"_meta":       uiMeta,
		},
		map[string]any{
			"name":        "validate_character",
			"description": "Validate an IDD character build against the rules (CP, attributes, headers, skills, faction, prerequisites, exclusions). Returns a full text report: legality, errors, warnings, CP breakdown, Vitality, tier, and traits. Use this before presenting any build as final.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
		},
		map[string]any{
			"name":        "get_catalog",
			"description": "List all IDD ruleset options as readable text: the 6 heritages, 4 factions, and all 27 headers with each header's CP cost, faction requirement (if any), and whether it grants the Arcane or Devout trait. Call this first to see what's available.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "get_header",
			"description": "Show the skills inside a single header (or the open skills): each skill's name, CP cost, attribute activation cost, and a one-line effect. Use this to see what a header offers before choosing skills. Pass the header name, or \"Open\" for the open skills.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		},
		map[string]any{
			"name":        "get_current_build",
			"description": "Read the character in the SHARED live builder session — the one shown in the browser builder head. Use this to see what the player currently has (including edits they made in the browser).",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "set_current_build",
			"description": "Replace the character in the SHARED live builder session with this build. The browser builder head updates live (via SSE) to show it. Returns the validation result. Use this to push a build you've assembled into the player's open builder.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
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
	case "get_header":
		return toolGetHeader(p.Arguments)
	case "get_current_build":
		return toolGetCurrent()
	case "set_current_build":
		return toolSetCurrent(p.Arguments)
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
	var b strings.Builder
	if res.Valid {
		fmt.Fprintf(&b, "VALID BUILD — %d/%d CP spent, %d remaining.\n", res.Total, res.Available, res.Remaining)
	} else {
		fmt.Fprintf(&b, "INVALID BUILD — %d error(s):\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Fprintf(&b, "  • %s\n", e)
		}
	}
	fmt.Fprintf(&b, "CP: attributes %d + headers %d + skills %d = %d of %d.\n",
		res.AttrCP, res.HeaderCP, res.SkillsCP, res.Total, res.Available)
	fmt.Fprintf(&b, "Vitality %d · Tier %s.\n", res.Vitality, res.Tier)
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	fmt.Fprintf(&b, "Traits: %s.", strings.Join(res.Traits, ", "))
	return textResult(b.String(), res), nil
}

var reMemberT = regexp.MustCompile(`(?i)member of (the [\w' ]+?)(?:\.|$| to)`)

func headerFaction(h *rules.Header) string {
	for _, p := range h.Prerequisites {
		if m := reMemberT.FindStringSubmatch(p); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func toolCatalog() (any, *rpcError) {
	var b strings.Builder
	var heritages, factions []string
	for _, h := range ruleset.Heritages {
		heritages = append(heritages, h.Name)
	}
	for _, f := range ruleset.Factions {
		factions = append(factions, f.Name)
	}
	fmt.Fprintf(&b, "IN DARKENED DREAMS — ruleset options\n\n")
	fmt.Fprintf(&b, "HERITAGES (free — every character has one): %s\n\n", strings.Join(heritages, ", "))
	fmt.Fprintf(&b, "FACTIONS (free — every character must join exactly one): %s\n", strings.Join(factions, ", "))
	fmt.Fprintf(&b, "Each faction unlocks 3 exclusive headers; a header with no faction requirement can be taken by any faction.\n\n")
	fmt.Fprintf(&b, "HEADERS (buy the header before its skills; then call get_header for its skill list):\n")
	for i := range ruleset.Headers {
		h := &ruleset.Headers[i]
		line := fmt.Sprintf("  %s — %d CP", h.Name, h.HeaderCost)
		if fac := headerFaction(h); fac != "" {
			line += " — requires " + fac
		}
		if len(h.GrantsTraits) > 0 {
			line += " — grants " + strings.Join(h.GrantsTraits, "/")
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nAlso: %d open skills (any character), via get_header \"Open\". Casters buy a Sphere under an Arcane header, then spells in it.", len(ruleset.OpenSkills))
	return textResult(b.String(), nil), nil
}

func toolGetHeader(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	skillLine := func(sb *strings.Builder, s rules.Skill) {
		line := fmt.Sprintf("  %s (%d CP)", s.Name, s.Cost)
		if a := attrStr(s.AttributeCost); a != "" {
			line += " [" + a + "]"
		}
		if s.SpellLike {
			line += " [spell-like]"
		}
		if len(s.Prerequisites) > 0 {
			line += " [needs " + strings.Join(s.Prerequisites, "/") + "]"
		}
		if e := eff(s.Description); e != "" {
			line += " — " + e
		}
		sb.WriteString(line + "\n")
	}
	var b strings.Builder
	if strings.EqualFold(a.Name, "open") {
		fmt.Fprintf(&b, "OPEN SKILLS (any character may buy these):\n")
		for _, s := range ruleset.OpenSkills {
			skillLine(&b, s)
		}
		return textResult(b.String(), nil), nil
	}
	var h *rules.Header
	for i := range ruleset.Headers {
		if strings.EqualFold(ruleset.Headers[i].Name, a.Name) {
			h = &ruleset.Headers[i]
			break
		}
	}
	if h == nil {
		return textResult(fmt.Sprintf("No header named %q. Use get_catalog for the list.", a.Name), nil), nil
	}
	fmt.Fprintf(&b, "%s — %d CP", h.Name, h.HeaderCost)
	if fac := headerFaction(h); fac != "" {
		fmt.Fprintf(&b, " — requires %s", fac)
	}
	if len(h.GrantsTraits) > 0 {
		fmt.Fprintf(&b, " — grants %s", strings.Join(h.GrantsTraits, "/"))
	}
	b.WriteString("\nSkills:\n")
	for _, s := range h.Skills {
		skillLine(&b, s)
	}
	return textResult(b.String(), nil), nil
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

func storePath(dir, name string) string { return filepath.Join(dir, safeFile(name)) }

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
