// Command a2ui-demo is a small Extension: it shows a panel in the sidebar of
// one Space in dense, and a tab in it, and changes them as you click.
//
//	go run ./examples/a2ui-demo --space NAME
//
// It creates Extension/a2ui-demo, listens on the socket its spec.endpoint
// names, and creates two Surfaces of kind ui it owns in the Space: one
// placed in the sidebar, and a tab. A frontend (dense)
// that shows them connects, says hello with the surfaces it wants, and gets a
// full createSurface for each. After that it's plain A2UI 1.0 both ways, one
// JSON message per line; dense's own messages are under a "dense" key.
// Stopping it with Ctrl-C (or SIGTERM) deletes Extension/a2ui-demo, and
// dennet then deletes the Surfaces it owns. With --keep, as for a restart,
// they stay, greyed in dense, until it runs again (or dennet deletes them
// after 10 minutes, as it does after a crash).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	api          = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/"
	denseCatalog = "https://dense.mkm.pub/a2ui/v1/catalog.json"
)

func main() {
	home, _ := os.UserHomeDir()
	state := filepath.Join(home, ".local", "state")
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		state = d
	}
	dennet := flag.String("socket", filepath.Join(state, "dennet", "dennet.sock"), "dennet's socket")
	ns := flag.String("namespace", "default", "namespace of the Space")
	space := flag.String("space", "", "name of the Space to show the panel in (required)")
	link := flag.String("link", "", "name of a tab (Surface) for the panel to link to")
	keep := flag.Bool("keep", false, "on exit, leave the surfaces in place, as for a restart")
	flag.Parse()
	if *space == "" {
		flag.Usage()
		os.Exit(2)
	}

	d := &demo{subs: map[*session]map[string]bool{}, clicks: map[string]int{}, link: *link}
	endpoint := filepath.Join(state, "dense", "ext", "a2ui-demo.sock")
	os.MkdirAll(filepath.Dir(endpoint), 0o700)
	os.Remove(endpoint)
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		log.Fatal(err)
	}
	os.Chmod(endpoint, 0o600)

	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", *dennet)
	}}}
	// Apply: create, or update the endpoint if it is already there.
	ext := map[string]any{"apiVersion": "den.mkm.pub/v1alpha1", "kind": "Extension",
		"metadata": map[string]any{"name": "a2ui-demo"}, "spec": map[string]any{"endpoint": endpoint}}
	var created struct {
		Metadata struct{ UID string } `json:"metadata"`
	}
	if call(c, "POST", api+*ns+"/extensions", ext, &created) == http.StatusConflict {
		call(c, "PATCH", api+*ns+"/extensions/a2ui-demo", map[string]any{"spec": ext["spec"]}, &created)
	}
	owner := []any{map[string]any{"apiVersion": "den.mkm.pub/v1alpha1", "kind": "Extension", "name": "a2ui-demo", "uid": created.Metadata.UID}}
	// Two Surfaces of kind ui, a sidebar section and a tab, both owned by the
	// Extension, so dennet deletes them some time after it stops for good.
	// One left by an old run is taken over. Closing the tab in dense deletes
	// it, and the next run makes it again.
	for _, placement := range []string{"sidebar", ""} {
		name, order := d.sidebar(*space), 10
		if placement == "" {
			name, order = d.tab(*space), 1000
		}
		surface := map[string]any{"apiVersion": "den.mkm.pub/v1alpha1", "kind": "Surface",
			"metadata": map[string]any{"name": name, "ownerReferences": owner},
			"spec": map[string]any{"title": "Demo", "space": *space, "order": order, "placement": placement,
				"kind": "ui", "ui": map[string]any{"extension": "a2ui-demo"}}}
		if call(c, "POST", api+*ns+"/surfaces", surface, nil) == http.StatusConflict {
			call(c, "PATCH", api+*ns+"/surfaces/"+name, map[string]any{"metadata": map[string]any{"ownerReferences": owner}}, nil)
		}
	}
	log.Printf("listening on %s; showing a panel and a tab in Space %s", endpoint, *space)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		if !*keep {
			// Deleting the Extension is enough: dennet deletes what it owns.
			req, _ := http.NewRequest("DELETE", api+*ns+"/extensions/a2ui-demo", nil)
			if resp, err := c.Do(req); err != nil {
				log.Print(err)
			} else {
				resp.Body.Close()
				log.Printf("deleted Extension a2ui-demo: %s", resp.Status)
			}
		}
		os.Remove(endpoint)
		os.Exit(0)
	}()

	go func() {
		for range time.Tick(2 * time.Second) {
			d.everyone(func(name string) any {
				return update(name, "/clock", time.Now().Format(time.TimeOnly))
			})
		}
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go d.serve(&session{conn: conn})
	}
}

// call sends a JSON request to dennet and returns the status code.
func call(c *http.Client, method, url string, body, into any) int {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if method == "PATCH" {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	}
	resp, err := c.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusConflict {
		log.Fatalf("%s %s: %s %s", method, url, resp.Status, out)
	}
	if into != nil {
		json.Unmarshal(out, into)
	}
	return resp.StatusCode
}

type demo struct {
	mu     sync.Mutex
	subs   map[*session]map[string]bool // subscribed surfaces of each session
	clicks map[string]int
	link   string
}

func (*demo) sidebar(space string) string { return "a2ui-demo-" + space }
func (*demo) tab(space string) string     { return "a2ui-demo-" + space + "-tab" }

type session struct {
	mu   sync.Mutex
	conn net.Conn
}

func (s *session) send(msg any) {
	b, _ := json.Marshal(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.Write(append(b, '\n'))
}

// serve handles one frontend. dennet connects too, to see that we're alive;
// it never says hello, so it gets nothing.
func (d *demo) serve(s *session) {
	defer func() {
		d.mu.Lock()
		delete(d.subs, s)
		d.mu.Unlock()
		s.conn.Close()
	}()
	r := bufio.NewScanner(s.conn)
	r.Buffer(nil, 1<<20)
	for r.Scan() {
		var msg struct {
			Dense *struct {
				Hello *struct {
					FrontendID string   `json:"frontendId"`
					Surfaces   []string `json:"surfaces"`
				} `json:"hello"`
				Subscribe, Unsubscribe *struct {
					SurfaceID string `json:"surfaceId"`
				}
			} `json:"dense"`
			Action *struct {
				Name      string         `json:"name"`
				SurfaceID string         `json:"surfaceId"`
				Context   map[string]any `json:"context"`
			} `json:"action"`
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(r.Bytes(), &msg); err != nil {
			log.Printf("bad line: %v", err)
			continue
		}
		switch {
		case msg.Dense != nil && msg.Dense.Hello != nil:
			log.Printf("hello from %s: %v", msg.Dense.Hello.FrontendID, msg.Dense.Hello.Surfaces)
			for _, name := range msg.Dense.Hello.Surfaces {
				d.subscribe(s, name)
			}
		case msg.Dense != nil && msg.Dense.Subscribe != nil:
			d.subscribe(s, msg.Dense.Subscribe.SurfaceID)
		case msg.Dense != nil && msg.Dense.Unsubscribe != nil:
			d.mu.Lock()
			delete(d.subs[s], msg.Dense.Unsubscribe.SurfaceID)
			d.mu.Unlock()
		case msg.Action != nil:
			log.Printf("action %s on %s: %v", msg.Action.Name, msg.Action.SurfaceID, msg.Action.Context)
			if msg.Action.Name == "bump" {
				d.mu.Lock()
				d.clicks["n"]++
				n := d.clicks["n"]
				d.mu.Unlock()
				d.everyone(func(name string) any { return update(name, "/clicks", clicked(n)) })
			}
		case msg.Error != nil:
			log.Printf("dense reports an error: %v", msg.Error)
		}
	}
}

// subscribe sends a full snapshot of a surface and keeps it updated.
func (d *demo) subscribe(s *session, name string) {
	d.mu.Lock()
	if d.subs[s] == nil {
		d.subs[s] = map[string]bool{}
	}
	d.subs[s][name] = true
	clicks := d.clicks["n"]
	d.mu.Unlock()
	s.send(d.snapshot(name, clicks))
}

// everyone sends msg(surface) to every session, for each surface it subscribed to.
func (d *demo) everyone(msg func(name string) any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for s, names := range d.subs {
		for name := range names {
			s.send(msg(name))
		}
	}
}

func clicked(n int) string { return fmt.Sprintf("Clicked %d times", n) }

func update(name, path string, value any) any {
	return map[string]any{"version": "1.0", "updateDataModel": map[string]any{"surfaceId": name, "path": path, "value": value}}
}

// snapshot is a whole surface: components and data model in one message, so
// a frontend that (re)connects needs nothing else.
func (d *demo) snapshot(name string, clicks int) any {
	text := func(id, text string) map[string]any {
		return map[string]any{"id": id, "component": "Text", "text": text}
	}
	components := []any{
		map[string]any{"id": "root", "component": "Column", "children": []string{"status", "clock", "clicks", "facts", "link", "buttons"}},
		map[string]any{"id": "clock", "component": "Text", "text": map[string]any{"@path": "/clock"}},
		map[string]any{"id": "clicks", "component": "Text", "variant": "caption", "text": map[string]any{"@path": "/clicks"}},
		map[string]any{"id": "status", "component": "StatusBadge", "kind": map[string]any{"@path": "/status/kind"}, "text": map[string]any{"@path": "/status/text"}},
		map[string]any{"id": "facts", "component": "Section", "title": "Facts", "child": "rows"},
		map[string]any{"id": "rows", "component": "KeyValue", "rows": map[string]any{"@path": "/facts"}},
		map[string]any{"id": "link", "component": "SurfaceLink", "surface": map[string]any{"@path": "/link"}, "showState": true},
		map[string]any{"id": "buttons", "component": "Row", "children": []string{"bump", "copy"}},
		map[string]any{"id": "bump", "component": "Button", "child": "bump_label", "action": map[string]any{"event": map[string]any{
			"name": "bump", "context": map[string]any{"clock": map[string]any{"@path": "/clock"}}}}},
		text("bump_label", "Click me"),
		map[string]any{"id": "copy", "component": "Button", "child": "copy_label", "action": map[string]any{"functionCall": map[string]any{
			"@call": "copyText", "args": map[string]any{"text": map[string]any{"@path": "/clock"}}}}},
		text("copy_label", "Copy time"),
	}
	return map[string]any{"version": "1.0", "createSurface": map[string]any{
		"surfaceId":  name,
		"catalogId":  denseCatalog,
		"components": components,
		"dataModel": map[string]any{
			"status": map[string]any{"kind": "ok", "text": "running"},
			"facts": []any{
				map[string]any{"key": "surface", "value": name},
				map[string]any{"key": "pid", "value": fmt.Sprint(os.Getpid())},
			},
			"clock":  time.Now().Format(time.TimeOnly),
			"clicks": clicked(clicks),
			"link":   d.link,
		},
	}}
}
