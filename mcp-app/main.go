// Command idd-mcp is the In Darkened Dreams MCP server. It speaks MCP over
// stdio (line-delimited JSON-RPC 2.0) and exposes character-building tools:
// discovery (get_catalog/get_header/get_heritage/get_sphere/find_ability),
// validation, a shared live build session, and character persistence.
//
// It also runs an opt-in localhost HTTP head (see http.go) serving a browser
// builder that shares the same live session; there is no in-chat UI panel.
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
	"time"

	"github.com/verveguy/idd/mcp-app/rules"
)

// defaultProtocol is the core MCP protocol version we fall back to in negotiation.
const defaultProtocol = "2025-11-25"

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
	// Capture our launcher's PID first thing, before any slow init, so the orphan
	// watcher can detect specifically when THAT parent dies (reparenting changes it).
	origPPID := os.Getppid()

	rs, err := rules.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal: load ruleset:", err)
		os.Exit(1)
	}
	ruleset = rs

	// Opt-in local HTTP head (localhost only): serves the browser builder + a
	// shared session both Claude (MCP) and the browser edit. Binds synchronously
	// (so builderURL is known before initialize) then serves alongside stdio.
	startHTTP()

	// Don't squat the HTTP port after our launcher (Claude Desktop) is gone. Normally
	// stdin EOF ends the read loop below and the process exits; this backstops the
	// case where the parent dies without closing our stdin (force-kill, reparenting) —
	// which is exactly how a stale server ends up holding :7777 for the next session.
	go watchParent(origPPID)

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

// watchParent exits the process once our original launcher (orig) has died — when
// the parent dies we're reparented, so getppid() no longer equals orig. This frees
// the HTTP port for the next instance instead of leaving a stale builder squatting
// it. If we were already orphaned at startup (orig==1, e.g. launched detached on
// purpose), there's nothing to watch.
func watchParent(orig int) {
	if orig <= 1 {
		return
	}
	for {
		time.Sleep(2 * time.Second)
		if os.Getppid() != orig {
			fmt.Fprintln(os.Stderr, "[idd] launcher exited; releasing port and shutting down")
			os.Exit(0)
		}
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
			"serverInfo":      map[string]any{"name": "in-darkened-dreams", "version": "1.2.6"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"instructions":    serverInstructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
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
	buildObj := map[string]any{"type": "object", "description": "An IDD character build object."}
	return []any{
		map[string]any{
			"name":        "validate_character",
			"description": "STATELESS legality check of an IDD build — full text report: legality, errors, warnings, CP breakdown, Vitality, tier, traits. IMPORTANT: this does NOT touch the player's live browser view. When a live builder session exists (the normal case), do NOT use this to check a build mid-construction — use patch_build instead, which validates AND shows the player. Reserve validate_character for when there is no live session, or a pure one-off check of an external build object.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
		},
		map[string]any{
			"name":        "get_builder_url",
			"description": "Return the exact URL of THIS chat's live browser builder (the page served by this server instance). Tell the player to open it to see/edit the character. Use this instead of assuming a port — it may not be the default if another instance was already running.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "get_catalog",
			"description": "List all IDD ruleset options as readable text: the 6 heritages, 4 factions, and all 27 headers with each header's CP cost, faction requirement (if any), and whether it grants the Arcane or Devout trait. Call this first to see what's available.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "get_header",
			"description": "Show the skills inside a single header (or the open skills): each skill's name, CP cost, attribute activation cost, and a one-line effect. Use this to see what a header offers before choosing skills. Pass the header name, or \"Open\" for the open skills. If a header grants a Sphere, use get_sphere to see that sphere's spells.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		},
		map[string]any{
			"name":        "get_heritage",
			"description": "Show one heritage's DRAWBACK and its purchasable heritage skills (name, CP cost, one-line effect). Choosing a heritage is free (0 CP), but its skills cost CP and are added to skills by name. Call this after get_catalog to see what a heritage actually grants before committing. Pass the heritage name.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		},
		map[string]any{
			"name":        "get_sphere",
			"description": "List the individually-purchasable SPELLS in one of the six magic spheres (Restoration, Evocation, Illusion, Conjuration, Alteration, Manipulation) — each spell's name, CP cost, per-cast attribute cost, and effect. A caster buys an Arcane header, then a \"The Sphere of X\" skill, then buys spells from that sphere by name (they only become legal once the sphere is owned). Pass the sphere name (\"Evocation\" or \"The Sphere of Evocation\").",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		},
		map[string]any{
			"name":        "find_ability",
			"description": "Search ALL purchasable things (header skills, open skills, sphere spells, heritage skills) and header names for a keyword, and report WHERE each match lives and what you must own first. Use this to turn a concept into mechanics — e.g. find_ability \"lightning\" (it's in the Storm Caller header, not a sphere), \"heal\", \"stealth\", \"armor\". Note: the ruleset may use different words (\"thunder\" for lightning, \"restoration\" for healing) — try synonyms if a search is empty.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"keyword": map[string]any{"type": "string"}}, "required": []string{"keyword"}},
		},
		map[string]any{
			"name":        "get_current_build",
			"description": "Read the character in the SHARED live builder session — the one shown in the browser builder head. Use this to see what the player currently has (including edits they made in the browser).",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "set_current_build",
			"description": "Push the WHOLE build to the SHARED live session — the player's browser page (see the server instructions for the URL) updates live via SSE. Use this to set/replace the entire character. Returns the validation result (hard-block rules).",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"build": buildObj}, "required": []string{"build"}},
		},
		map[string]any{
			"name":        "patch_build",
			"description": "Apply a PARTIAL character sheet to the SHARED live session — deep-merged into the current build, so the player's browser updates live AND you get the merged build's validation back. THIS IS YOUR PRIMARY BUILD TOOL: call it the moment you decide each piece, starting the instant you have a name+heritage — don't wait until the character is complete. Push {\"name\":\"Moonbeam\",\"heritage\":\"Solari\"}, then {\"faction\":\"...\"}, then {\"headers\":[\"Warrior\"]}, then {\"skills\":[{\"name\":\"Heart of the Lion\"}]}, then {\"attributes\":{\"Fire\":4}} — each shows up live as you go. Objects merge (patch one attribute at a time); arrays (headers, skills) and scalars replace. The live session already has a default 35 CP budget, so you can patch into it from empty. Use this instead of validate_character during a build.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"patch": map[string]any{"type": "object", "description": "A partial IDD build: any subset of name, heritage, faction, cp_sources, attributes, headers, skills."}}, "required": []string{"patch"}},
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
	case "get_sphere":
		return toolGetSphere(p.Arguments)
	case "get_heritage":
		return toolGetHeritage(p.Arguments)
	case "find_ability":
		return toolFindAbility(p.Arguments)
	case "get_builder_url":
		return toolBuilderURL()
	case "get_current_build":
		return toolGetCurrent()
	case "set_current_build":
		return toolSetCurrent(p.Arguments)
	case "patch_build":
		return toolPatch(p.Arguments)
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

	fmt.Fprintf(&b, "ATTRIBUTES (all 5 start at 2, for free): Air, Earth, Fire, Water, Void.\n")
	fmt.Fprintf(&b, "Raising an attribute to level N costs N CP, and you pay every step: 2→3 = 3 CP; 2→4 = 3+4 = 7 CP; 2→5 = 3+4+5 = 12 CP.\n")
	fmt.Fprintf(&b, "Vitality = ceil((Earth + Void) / 2). Attribute costs printed on skills/spells like [1 Fire] are spent DURING PLAY each event — they are NOT CP.\n\n")

	fmt.Fprintf(&b, "HERITAGES (free — every character has one): %s. Call get_heritage <name> for a heritage's drawback + buyable skills.\n\n", strings.Join(heritages, ", "))

	fmt.Fprintf(&b, "FACTIONS (free — every character must join exactly one): %s\n", strings.Join(factions, ", "))
	fmt.Fprintf(&b, "You may only pick ONE faction, and it gates your headers. Headers grouped by the faction that unlocks them:\n\n")

	fmt.Fprintf(&b, "HEADERS (buy the header before its skills; then call get_header <name> for its skill list):\n")
	writeHeaderLine := func(h *rules.Header) {
		line := fmt.Sprintf("    %s — %d CP", h.Name, h.HeaderCost)
		if len(h.GrantsTraits) > 0 {
			line += " — grants " + strings.Join(h.GrantsTraits, "/")
		}
		b.WriteString(line + "\n")
	}
	// Group by faction (ruleset order), then a faction-free bucket last.
	for _, f := range ruleset.Factions {
		fmt.Fprintf(&b, "  %s unlocks:\n", f.Name)
		for i := range ruleset.Headers {
			if headerFaction(&ruleset.Headers[i]) == f.Name {
				writeHeaderLine(&ruleset.Headers[i])
			}
		}
	}
	fmt.Fprintf(&b, "  Any faction (no faction requirement):\n")
	for i := range ruleset.Headers {
		if headerFaction(&ruleset.Headers[i]) == "" {
			writeHeaderLine(&ruleset.Headers[i])
		}
	}
	fmt.Fprintf(&b, "\nAlso: %d open skills (any character), via get_header \"Open\". Casters buy an Arcane header, then a Sphere skill in it, then individual spells (get_sphere).\n", len(ruleset.OpenSkills))
	fmt.Fprintf(&b, "Looking for a specific ability by theme (e.g. \"lightning\", \"heal\", \"stealth\")? Call find_ability <keyword> to locate which header/sphere it lives in.")
	return textResult(b.String(), nil), nil
}

func writeSkill(sb *strings.Builder, s rules.Skill) {
	line := fmt.Sprintf("  %s (%d CP)", s.Name, s.Cost)
	if a := attrStr(s.AttributeCost); a != "" {
		line += " [" + a + "]"
	}
	if s.Repeatable {
		if s.PurchaseLimit != nil {
			line += fmt.Sprintf(" [repeatable up to %d×]", *s.PurchaseLimit)
		} else {
			line += " [repeatable]"
		}
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
	if sp := sphereOfU(s.Name); sp != "" {
		line += "  (→ call get_sphere \"" + sp + "\" to list its purchasable spells)"
	}
	sb.WriteString(line + "\n")
}

func toolGetHeader(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name   string `json:"name"`
		Header string `json:"header"` // alias: the build JSON uses this key
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	if strings.TrimSpace(a.Name) == "" {
		a.Name = strings.TrimSpace(a.Header)
	}
	if strings.TrimSpace(a.Name) == "" {
		return textResult("Which header? Pass name, e.g. {\"name\":\"Wizard\"}, or {\"name\":\"Open\"} for open skills. See get_catalog for the list.", nil), nil
	}
	var b strings.Builder
	if strings.EqualFold(a.Name, "open") {
		fmt.Fprintf(&b, "OPEN SKILLS (any character may buy these):\n")
		for _, s := range ruleset.OpenSkills {
			writeSkill(&b, s)
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
		writeSkill(&b, s)
	}
	return textResult(b.String(), nil), nil
}

func toolGetHeritage(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name     string `json:"name"`
		Heritage string `json:"heritage"` // alias: the build JSON uses this key
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	q := strings.ToLower(strings.TrimSpace(a.Name))
	if q == "" {
		q = strings.ToLower(strings.TrimSpace(a.Heritage))
	}
	heritageNames := func() string {
		var names []string
		for _, x := range ruleset.Heritages {
			names = append(names, x.Name)
		}
		return strings.Join(names, ", ")
	}
	if q == "" {
		return textResult("Which heritage? Pass name, e.g. {\"name\":\"Grelkyn\"}. The six: "+heritageNames()+". Choosing one is free (0 CP).", nil), nil
	}
	var h *rules.Heritage
	for i := range ruleset.Heritages {
		hn := strings.ToLower(ruleset.Heritages[i].Name)
		if hn == q || strings.Contains(hn, q) {
			h = &ruleset.Heritages[i]
			break
		}
	}
	if h == nil {
		return textResult(fmt.Sprintf("No heritage named %q. The six heritages: %s. Choosing one is free (0 CP).", a.Name+a.Heritage, heritageNames()), nil), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — choosing a heritage is FREE (0 CP).\n", h.Name)
	if len(h.Disadvantages) > 0 {
		fmt.Fprintf(&b, "Drawback: %s\n", strings.Join(h.Disadvantages, " "))
	} else {
		b.WriteString("Drawback: none.\n")
	}
	if len(h.HeritageSkills) > 0 {
		b.WriteString("Heritage skills — add each you want to skills by name (even 0-CP ones are NOT auto-granted; list them explicitly):\n")
		for _, s := range h.HeritageSkills {
			line := fmt.Sprintf("  %s (%d CP)", s.Name, s.Cost)
			if e := eff(s.Description); e != "" {
				line += " — " + e
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("No purchasable heritage skills.\n")
	}
	return textResult(b.String(), nil), nil
}

var reSphereName = regexp.MustCompile(`(?i)\b(the\s+)?sphere\s+of\s+`)

func toolGetSphere(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name   string `json:"name"`
		Sphere string `json:"sphere"` // alias
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	if strings.TrimSpace(a.Name) == "" {
		a.Name = strings.TrimSpace(a.Sphere)
	}
	key := strings.TrimSpace(reSphereName.ReplaceAllString(a.Name, ""))
	var spells []rules.Skill
	var name string
	for k, v := range ruleset.Spells {
		if strings.EqualFold(k, key) || strings.EqualFold(k, a.Name) {
			spells, name = v, k
			break
		}
	}
	if spells == nil {
		var keys []string
		for k := range ruleset.Spells {
			keys = append(keys, k)
		}
		return textResult(fmt.Sprintf("No sphere named %q. The six spheres are: %s. Casters own a sphere via a skill like \"The Sphere of Evocation\" under an Arcane header.", a.Name, strings.Join(keys, ", ")), nil), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Spells of %s — buy each you want INDIVIDUALLY (add it by name to skills, like any skill). Each is only purchasable once you own \"The Sphere of %s\"; the CP is the buy cost, the [attribute] is what it costs to cast in play:\n", name, name)
	for _, s := range spells {
		writeSkill(&b, s)
	}
	return textResult(b.String(), nil), nil
}

// toolFindAbility searches every purchasable thing (header skills, open skills,
// sphere spells, heritage skills) plus header names for a keyword, and reports
// WHERE each match lives + what you must own to buy it. This bridges a player's
// concept ("lightning", "heal") to the mechanical items, which are otherwise
// scattered across headers and spheres with no single index.
func toolFindAbility(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Name    string `json:"name"`
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &rpcError{Code: -32602, Message: "bad params"}
	}
	kw := strings.ToLower(strings.TrimSpace(a.Keyword))
	if kw == "" {
		kw = strings.ToLower(strings.TrimSpace(a.Name))
	}
	if kw == "" {
		return textResult("Pass a keyword to search for, e.g. {\"keyword\":\"lightning\"}.", nil), nil
	}
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), kw) }
	// Two buckets: items whose NAME matches (what the player usually wants) rank
	// above items that only mention the keyword in their effect text. Without this
	// split, a common word like "fire" buries the actual "Fire Storm" spell under
	// dozens of skills that merely mention fire in prose.
	var nameLines, descLines []string
	add := func(nameHit bool, name string, cost int, where string) {
		l := fmt.Sprintf("  %s (%d CP) — %s", name, cost, where)
		if nameHit {
			nameLines = append(nameLines, l)
		} else {
			descLines = append(descLines, l)
		}
	}
	consider := func(name, desc string, cost int, where string) {
		if has(name) {
			add(true, name, cost, where)
		} else if has(desc) {
			add(false, name, cost, where)
		}
	}
	// Header names themselves (name-only; a header has no "description" here).
	for i := range ruleset.Headers {
		h := &ruleset.Headers[i]
		if has(h.Name) || has(h.Notes) {
			w := "a HEADER"
			if fac := headerFaction(h); fac != "" {
				w += " (requires " + fac + ")"
			}
			add(has(h.Name), h.Name, h.HeaderCost, w)
		}
	}
	// Skills under each header.
	for i := range ruleset.Headers {
		h := &ruleset.Headers[i]
		for _, s := range h.Skills {
			consider(s.Name, s.Description, s.Cost, "skill in header \""+h.Name+"\" (buy that header first)")
		}
	}
	// Open skills.
	for _, s := range ruleset.OpenSkills {
		consider(s.Name, s.Description, s.Cost, "OPEN skill (no header needed)")
	}
	// Spells, by sphere.
	for sphere, spells := range ruleset.Spells {
		for _, s := range spells {
			consider(s.Name, s.Description, s.Cost, "spell in the Sphere of "+sphere+" (buy an Arcane header + \"The Sphere of "+sphere+"\" first)")
		}
	}
	// Heritage skills.
	for _, h := range ruleset.Heritages {
		for _, s := range h.HeritageSkills {
			consider(s.Name, s.Description, s.Cost, h.Name+" heritage skill (only if you are "+h.Name+")")
		}
	}
	if len(nameLines)+len(descLines) == 0 {
		return textResult(fmt.Sprintf("No header/skill/spell matches %q. Try a broader or different word (the ruleset may name it differently — e.g. \"thunder\" not \"lightning\", \"restoration\" not \"heal\").", kw), nil), nil
	}
	// Always keep every name match; fill the rest of the budget with desc matches.
	const cap = 40
	lines := nameLines
	trunc := ""
	if room := cap - len(lines); room < len(descLines) {
		if room > 0 {
			lines = append(lines, descLines[:room]...)
		}
		trunc = fmt.Sprintf("\n…and %d more that only MENTION %q in their effect text (narrow the keyword).", len(descLines)-max(room, 0), kw)
	} else {
		lines = append(lines, descLines...)
	}
	return textResult(fmt.Sprintf("Things matching %q — name matches first, then effect-text mentions; the CP is the buy cost, \"where\" tells you what to own first:\n%s%s", kw, strings.Join(lines, "\n"), trunc), nil), nil
}

// serverInstructions is sent in the initialize result — it tells the model how
// to use this server: build via the tools, push to the shared session so the
// browser updates live, and point players at the browser URL (no in-chat panel).
func serverInstructions() string {
	url := builderURL
	if url == "" {
		url = "(the local browser builder is disabled on this install)"
	}
	return "This server powers a LIVE In Darkened Dreams (IDD) LARP character builder that is shared with a web page in the player's browser at " + url + " (call get_builder_url anytime for this exact address — tell the player to open it; it does not open by itself).\n\n" +
		"WELCOME FIRST — before anything else, your VERY FIRST message in an IDD builder conversation must greet the player and give them this exact URL, e.g.:\n" +
		"    \"Welcome to the In Darkened Dreams character builder! 🎲 Open " + url + " in your browser to watch your character come together live as we build it. What character do you have in mind?\"\n" +
		"  Use the EXACT url above (it may not be the default port if another builder was already running — do not guess :7777). If the player says the page looks empty or stale, it's almost certainly open on the wrong port: re-send them this exact URL and have them confirm the address bar matches. Then proceed.\n\n" +
		"WORK LIVE — this is the most important rule. The player is watching that browser page, so build IN THE OPEN, not silently. The MOMENT you decide any piece of the character, call patch_build with just that piece so it appears on their screen. Do NOT research everything, assemble the whole sheet in your head, and push it once at the end — that leaves the player staring at an empty page the whole time. Concretely, in this order as you go:\n" +
		"    1. As soon as you have the name + heritage, patch_build {\"name\":\"...\",\"heritage\":\"...\"}. Do this BEFORE you research skills.\n" +
		"    2. Patch the faction once chosen.\n" +
		"    3. Patch each header the moment you pick it: {\"headers\":[...]}.\n" +
		"    4. Patch skills/spells as you add them: {\"skills\":[{\"name\":\"...\"}]}.\n" +
		"    5. Patch attributes as you allocate them: {\"attributes\":{\"Fire\":4}}.\n" +
		"  Interleave these patches with your discovery calls (get_header, find_ability, get_sphere): decide a piece → patch it → research the next piece. The live session already starts with a sensible default budget (30 starting + 5 history = 35 CP) and every attribute at 2, so you can patch into it from empty with no setup — you do NOT need cp_sources to start.\n" +
		"- patch_build both VALIDATES (it returns the merged build's validation) AND updates the player's view, so during a build you never need validate_character — that one is a STATELESS check the player cannot see. Do not do your legality-checking with the invisible tool and then surprise the player with a finished sheet; let each patch be the check. (Objects merge — patch one attribute at a time; arrays like headers/skills and scalars replace. set_current_build replaces the whole sheet at once — use it only to load a saved character.)\n" +
		"- Before changing an EXISTING build, call get_current_build first to read what the player may have edited in the browser, so you don't overwrite it.\n" +
		"- The group uses hard-block rules, so resolve any errors a patch reports before calling a build final. There is NO in-chat builder panel — if the player wants to see it, tell them to open " + url + " in a browser (it does not open by itself).\n\n" +
		"Reference (use the discovery tools; don't recite from memory):\n" +
		"- get_catalog and get_header show heritages, factions, headers, and skills. Heritage and Faction are FREE (0 CP); CP is spent only on attributes, headers, and skills/spells. Raising an attribute to level N costs N CP cumulatively (2→5 = 3+4+5 = 12).\n" +
		"- CONCEPT → MECHANICS: when the player wants a themed ability (\"lightning\", \"heal\", \"stealth\"), call find_ability with that keyword — it tells you which header or sphere the ability lives in and what to buy first. Abilities are scattered across headers AND spheres; don't assume a theme maps to a sphere (e.g. lightning is a Storm Caller header skill, not a spell). Use get_heritage to see a heritage's drawback + buyable skills.\n" +
		"- MAGIC: a caster buys an Arcane header, then buys a Sphere skill (e.g. \"The Sphere of Evocation\", ~2 CP), then buys INDIVIDUAL spells from that sphere. Each spell is a separate purchasable skill with its own CP cost (add it to skills by name) and a per-cast attribute cost. Owning the sphere does NOT auto-grant its spells — call get_sphere to list them and buy the ones you want (they only validate once the sphere is owned)."
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

