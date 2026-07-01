package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SurfaceEvent is one user interaction with a rendered surface (the canvas loop).
type SurfaceEvent struct {
	Kind   string `json:"kind"`             // click | change | ...
	Target string `json:"target,omitempty"` // element id / name / tag
	Value  string `json:"value,omitempty"`  // text / input value
	At     int64  `json:"at"`
}

type surface struct {
	html   string
	events []SurfaceEvent
}

// Surfaces holds AI-rendered canvases and the interaction events users send back.
// Whoever renders them (the daemon) serves Handler() over HTTP; interactions fire
// onEvent, which drives the MCP change signal so a subscribed AI is pushed them.
type Surfaces struct {
	base    string
	onEvent func()
	mu      sync.Mutex
	m       map[string]*surface
}

func NewSurfaces(base string, onEvent func()) *Surfaces {
	return &Surfaces{base: base, onEvent: onEvent, m: map[string]*surface{}}
}

// Render stores (or replaces) a surface's HTML and returns its URL.
func (s *Surfaces) Render(name, html string) string {
	s.mu.Lock()
	su := s.m[name]
	if su == nil {
		su = &surface{}
		s.m[name] = su
	}
	su.html = html
	s.mu.Unlock()
	return strings.TrimRight(s.base, "/") + "/s/" + name
}

// Events returns a surface's interaction events (backs the resource read).
func (s *Surfaces) Events(name string) []SurfaceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if su := s.m[name]; su != nil {
		return append([]SurfaceEvent(nil), su.events...)
	}
	return nil
}

// Handler serves a surface's HTML (GET /s/{name}) with interaction-capture JS
// injected, and records interactions (POST /s/{name}/event).
func (s *Surfaces) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /s/{name}", s.serve)
	mux.HandleFunc("POST /s/{name}/event", s.event)
	return mux
}

func (s *Surfaces) serve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	su := s.m[name]
	s.mu.Unlock()
	if su == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, su.html)
	fmt.Fprintf(w, injectJS, name)
}

func (s *Surfaces) event(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var ev SurfaceEvent
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&ev)
	ev.At = time.Now().Unix()
	s.mu.Lock()
	su := s.m[name]
	if su == nil {
		su = &surface{}
		s.m[name] = su
	}
	su.events = append(su.events, ev)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
	if s.onEvent != nil {
		s.onEvent()
	}
}

// injectJS is appended to every served surface: it reports clicks and input
// changes back to the control plane, closing the loop to a subscribed AI.
const injectJS = `
<script>(function(){
  function send(ev){fetch('/s/%[1]s/event',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(ev)});}
  document.addEventListener('click',function(e){var t=e.target;send({kind:'click',target:t.id||t.name||t.tagName,value:(t.value||t.textContent||'').slice(0,120)});});
  document.addEventListener('change',function(e){var t=e.target;send({kind:'change',target:t.id||t.name||t.tagName,value:(t.value||'').slice(0,200)});});
})();</script>`
