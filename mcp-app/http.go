package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/verveguy/idd/mcp-app/rules"
)

//go:embed ui/head.html
var headHTML string

// ---- shared session: one "current build" both Claude (MCP) and browser (HTTP) edit ----

type hub struct {
	mu      sync.Mutex
	build   map[string]any
	version int
	subs    map[chan string]bool
}

var session = &hub{build: defaultBuild(), subs: map[chan string]bool{}}

func defaultBuild() map[string]any {
	return map[string]any{
		"name": "", "heritage": "", "faction": "",
		"cp_sources": map[string]any{"starting": 30, "history": 5},
		"attributes": map[string]any{"Air": 2, "Earth": 2, "Fire": 2, "Water": 2, "Void": 2},
		"headers":    []any{}, "skills": []any{},
	}
}

func (h *hub) snapshot() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"version": h.version, "src": "", "build": h.build})
	return b
}

func (h *hub) get() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, _ := json.Marshal(h.build)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func (h *hub) set(build map[string]any, src string) int {
	h.mu.Lock()
	h.build = build
	h.version++
	v := h.version
	msg, _ := json.Marshal(map[string]any{"version": v, "src": src, "build": build})
	subs := make([]chan string, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- string(msg):
		default:
		}
	}
	return v
}

func clone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var c map[string]any
	_ = json.Unmarshal(b, &c)
	return c
}

// deepMerge merges src into dst: nested objects merge recursively; arrays and
// scalars replace. This is how a partial character sheet is applied.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				deepMerge(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}

func (h *hub) patch(p map[string]any, src string) (int, map[string]any) {
	h.mu.Lock()
	deepMerge(h.build, p)
	h.version++
	v := h.version
	merged := clone(h.build)
	msg, _ := json.Marshal(map[string]any{"version": v, "src": src, "build": h.build})
	subs := make([]chan string, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- string(msg):
		default:
		}
	}
	return v, merged
}

func (h *hub) subscribe() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.subs[ch] = true
	h.mu.Unlock()
	return ch
}
func (h *hub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	close(ch)
	h.mu.Unlock()
}

func validateMap(m map[string]any) rules.Result {
	b, _ := json.Marshal(m)
	var bd rules.Build
	_ = json.Unmarshal(b, &bd)
	return ruleset.Validate(bd)
}

func summaryText(res rules.Result) string {
	if res.Valid {
		return fmt.Sprintf("VALID — %d/%d CP (%d left), Vitality %d, %s.",
			res.Total, res.Available, res.Remaining, res.Vitality, res.Tier)
	}
	return fmt.Sprintf("INVALID — %d error(s): %s", len(res.Errors), strings.Join(res.Errors, " | "))
}

// ---- MCP tools that read/write the shared session (browser head sees changes live) ----

// Each server instance is self-contained: it owns its own port + session, and
// reports its actual URL so Claude can tell the player exactly which page to open
// (the one served by THIS instance — the same process handling these tool calls).
func curResult(m map[string]any, prefix string) map[string]any {
	b, _ := json.MarshalIndent(m, "", "  ")
	return textResult(prefix+string(b)+"\n\n"+summaryText(validateMap(m)), m)
}

func toolGetCurrent() (any, *rpcError) {
	return curResult(session.get(), "Current shared build:\n"), nil
}

func toolSetCurrent(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Build map[string]any `json:"build"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Build == nil {
		return nil, &rpcError{Code: -32602, Message: "invalid build"}
	}
	session.set(a.Build, "claude")
	return curResult(a.Build, "Pushed to the live builder ("+builderURL+"):\n"), nil
}

func toolPatch(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Patch map[string]any `json:"patch"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Patch == nil {
		return nil, &rpcError{Code: -32602, Message: "invalid patch (expected a partial character sheet under \"patch\")"}
	}
	_, merged := session.patch(a.Patch, "claude")
	return curResult(merged, "Patched the live builder ("+builderURL+"):\n"), nil
}

func toolBuilderURL() (any, *rpcError) {
	if builderURL == "" {
		return textResult("The local browser builder is disabled on this install.", nil), nil
	}
	return textResult("The character builder is open at "+builderURL+" — share this exact URL with the player.", map[string]any{"url": builderURL}), nil
}

// ---- HTTP server (opt-in, localhost only) ----

func httpAddr() string {
	a := os.Getenv("IDD_HTTP_ADDR")
	if a == "off" {
		return ""
	}
	if a == "" {
		return "127.0.0.1:7777"
	}
	return a
}

// builderURL is the actual URL this instance's browser head is served at (set once
// the listener is bound, possibly on a fallback port if the preferred one is taken).
var builderURL string

// listenWithFallback binds the preferred addr, or the next free port after it, so
// a second instance (e.g. a stale process holding the port) still gets a working
// head on its own port instead of silently having none.
func listenWithFallback(addr string) (net.Listener, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return net.Listen("tcp", addr)
	}
	base, err := strconv.Atoi(portStr)
	if err != nil {
		return net.Listen("tcp", addr)
	}
	var lastErr error
	for i := 0; i < 20; i++ {
		if l, e := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(base+i))); e == nil {
			return l, nil
		} else {
			lastErr = e
		}
	}
	return nil, lastErr
}

// startHTTP binds synchronously (so builderURL is known before initialize is
// answered) and serves in the background.
func startHTTP() {
	addr := httpAddr()
	if addr == "" {
		return
	}
	ln, err := listenWithFallback(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[idd] http disabled: %v\n", err)
		return
	}
	builderURL = "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(os.Stderr, "[idd] builder at %s\n", builderURL)
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHead)
	mux.HandleFunc("/api/build", apiBuild)
	mux.HandleFunc("/api/patch", apiPatch)
	mux.HandleFunc("/api/events", apiEvents)
	mux.HandleFunc("/api/validate", apiValidate)
	mux.HandleFunc("/api/catalog", apiCatalog)
	mux.HandleFunc("/api/characters", apiCharacters)
	go func() { _ = http.Serve(ln, mux) }()
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func serveHead(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	html := strings.Replace(headHTML, "/*__IDD_DATA__*/", uiDataJSON(), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func apiBuild(w http.ResponseWriter, r *http.Request) {
	cors(w)
	switch r.Method {
	case http.MethodOptions:
		return
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(session.snapshot())
	case http.MethodPost:
		var body struct {
			Build map[string]any `json:"build"`
			Src   string         `json:"src"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Build == nil {
			http.Error(w, "bad build", 400)
			return
		}
		v := session.set(body.Build, body.Src)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": v})
	default:
		http.Error(w, "method", 405)
	}
}

func apiPatch(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	var body struct {
		Patch map[string]any `json:"patch"`
		Src   string         `json:"src"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Patch == nil {
		http.Error(w, "bad patch", 400)
		return
	}
	v, merged := session.patch(body.Patch, body.Src)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": v, "build": merged})
}

func apiEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no stream", 500)
		return
	}
	cors(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := session.subscribe()
	defer session.unsubscribe(ch)
	fmt.Fprintf(w, "data: %s\n\n", session.snapshot())
	fl.Flush()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		}
	}
}

func apiValidate(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	var body struct {
		Build map[string]any `json:"build"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(validateMap(body.Build))
}

func apiCatalog(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(uiDataJSON()))
}

func apiCharacters(w http.ResponseWriter, r *http.Request) {
	cors(w)
	dir, err := storeDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	switch r.Method {
	case http.MethodGet:
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				names = append(names, strings.TrimSuffix(e.Name(), ".json"))
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"names": names})
	case http.MethodPost:
		var body struct {
			Build json.RawMessage `json:"build"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Build) == 0 {
			http.Error(w, "missing build", 400)
			return
		}
		var nh struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(body.Build, &nh)
		if err := os.WriteFile(storePath(dir, nh.Name), body.Build, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": nh.Name})
	default:
		http.Error(w, "method", 405)
	}
}
