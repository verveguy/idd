package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

func toolGetCurrent() (any, *rpcError) {
	m := session.get()
	b, _ := json.MarshalIndent(m, "", "  ")
	return textResult("Current shared build:\n"+string(b)+"\n\n"+summaryText(validateMap(m)), m), nil
}

func toolSetCurrent(args json.RawMessage) (any, *rpcError) {
	var a struct {
		Build map[string]any `json:"build"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Build == nil {
		return nil, &rpcError{Code: -32602, Message: "invalid build"}
	}
	session.set(a.Build, "claude")
	return textResult("Pushed to the live builder. "+summaryText(validateMap(a.Build)), a.Build), nil
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

func startHTTP(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHead)
	mux.HandleFunc("/api/build", apiBuild)
	mux.HandleFunc("/api/events", apiEvents)
	mux.HandleFunc("/api/validate", apiValidate)
	mux.HandleFunc("/api/catalog", apiCatalog)
	mux.HandleFunc("/api/characters", apiCharacters)
	fmt.Fprintf(os.Stderr, "[idd] http head on http://%s\n", addr)
	if err := (&http.Server{Addr: addr, Handler: mux}).ListenAndServe(); err != nil {
		// graceful: port already taken (another instance) — stdio/MCP still works.
		fmt.Fprintf(os.Stderr, "[idd] http disabled: %v\n", err)
	}
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
