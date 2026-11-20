package server

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestExtensions checks that an Extension is Connected while its endpoint
// listens, Disconnected once it stops, and deleted after the grace period
// together with the Surfaces it owns; and that a ui Surface needs an
// extension, and one in the sidebar can't be glued.
func TestExtensions(t *testing.T) {
	old := extensionGrace
	extensionGrace = time.Second
	t.Cleanup(func() { extensionGrace = old })

	c, dir := start(t)
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/"
	do := func(method, url, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, base+url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	eventually := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; !ok(); i++ {
			if i == 200 {
				t.Fatal(what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	phase := func() any {
		_, m := do("GET", "extensions/x", "")
		st, _ := m["status"].(map[string]any)
		return st["phase"]
	}

	// The extension: accepts connections and keeps them until stopped.
	endpoint := filepath.Join(dir, "x.sock")
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()

	code, ext := do("POST", "extensions", `{"kind":"Extension","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"endpoint":"`+endpoint+`"}}`)
	if code != 201 {
		t.Fatalf("POST extension: %d %v", code, ext)
	}
	uid := ext["metadata"].(map[string]any)["uid"].(string)
	owner := `"ownerReferences":[{"apiVersion":"den.mkm.pub/v1alpha1","kind":"Extension","name":"x","uid":"` + uid + `"}]`
	if code, m := do("POST", "surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"u",`+owner+`},"spec":{"space":"s","placement":"sidebar","kind":"ui","ui":{"extension":"x"}}}`); code != 201 {
		t.Fatalf("POST sidebar surface: %d %v", code, m)
	}
	if code, m := do("POST", "surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"tab",`+owner+`},"spec":{"space":"s","kind":"ui","ui":{"extension":"x"}}}`); code != 201 {
		t.Fatalf("POST ui surface: %d %v", code, m)
	}
	if code, _ := do("POST", "surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"bad"},"spec":{"space":"s","kind":"ui"}}`); code != 422 {
		t.Errorf("ui Surface without extension: got %d, want 422", code)
	}
	if code, _ := do("POST", "surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"glued"},"spec":{"space":"s","placement":"sidebar","beside":"tab","kind":"ui","ui":{"extension":"x"}}}`); code != 422 {
		t.Errorf("glued Surface in the sidebar: got %d, want 422", code)
	}
	if code, _ := do("POST", "surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"where"},"spec":{"space":"s","placement":"dock","kind":"ui","ui":{"extension":"x"}}}`); code != 422 {
		t.Errorf("Surface placed in no known place: got %d, want 422", code)
	}
	eventually("not Connected", func() bool { return phase() == "Connected" })

	ln.Close()
	mu.Lock()
	for _, conn := range conns {
		conn.Close()
	}
	mu.Unlock()
	eventually("not Disconnected", func() bool { return phase() == "Disconnected" })
	eventually("Extension not deleted", func() bool { code, _ := do("GET", "extensions/x", ""); return code == 404 })
	eventually("sidebar Surface not deleted", func() bool { code, _ := do("GET", "surfaces/u", ""); return code == 404 })
	eventually("ui Surface not deleted", func() bool { code, _ := do("GET", "surfaces/tab", ""); return code == 404 })
}
