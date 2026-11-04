package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDenProtocol drives dennet over its Unix socket with plain HTTP, the way
// den does: create, single-object watch, status PATCH with a uid
// precondition, stale watches and deletion.
func TestDenProtocol(t *testing.T) {
	c, dir := start(t, "")
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/dens"
	do := func(method, url, ctype, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	if _, err := os.Stat(filepath.Join(dir, "token")); !os.IsNotExist(err) {
		t.Fatalf("token file created without --tcp: %v", err)
	}

	code, den := do("POST", base, "application/json",
		`{"apiVersion":"den.mkm.pub/v1alpha1","kind":"Den","metadata":{"name":"build"},"spec":{"command":["/bin/sh"]},"status":{"phase":"Running"}}`)
	if code != 201 {
		t.Fatalf("create: %d %v", code, den)
	}
	md, spec := den["metadata"].(map[string]any), den["spec"].(map[string]any)
	uid, rv := md["uid"].(string), md["resourceVersion"].(string)
	if spec["restartPolicy"] != "Never" || md["generation"] != 1.0 || den["status"] != nil && len(den["status"].(map[string]any)) != 0 {
		t.Fatalf("create should default spec, set generation 1 and drop status: %v", den)
	}

	events := watch(t, c, base+"?watch=true&fieldSelector=metadata.name%3Dbuild&resourceVersion="+rv)

	patch := `{"metadata":{"uid":"` + uid + `"},"status":{"phase":"Running","pid":42}}`
	if code, m := do("PATCH", base+"/build/status", "application/merge-patch+json", patch); code != 200 {
		t.Fatalf("status patch: %d %v", code, m)
	}
	if e := next(t, events); e.Type != "MODIFIED" || e.Object["status"].(map[string]any)["phase"] != "Running" {
		t.Fatalf("want MODIFIED with phase Running, got %v", e)
	}

	// dens/log asks the den on status.socket for its history.
	denSock := filepath.Join(dir, "d")
	fakeDen(t, denSock, "\x1b[31mhi\x1b[0m\n")
	if code, m := do("PATCH", base+"/build/status", "application/merge-patch+json", `{"status":{"socket":"`+denSock+`"}}`); code != 200 {
		t.Fatalf("status patch: %d %v", code, m)
	}
	next(t, events)
	resp, err := c.Get(base + "/build/log?color=true")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "\x1b[31mhi\x1b[0m\n" {
		t.Fatalf("log: %d %q", resp.StatusCode, body)
	}
	if code, _ := do("GET", base+"/nope/log", "", ""); code != 404 {
		t.Fatalf("log of a missing den: want 404, got %d", code)
	}

	stale := `{"metadata":{"uid":"00000000-0000-0000-0000-000000000000"},"status":{"phase":"Exited"}}`
	if code, m := do("PATCH", base+"/build/status", "application/merge-patch+json", stale); code != 409 {
		t.Fatalf("status patch with stale uid: want 409, got %d %v", code, m)
	}

	if e := next(t, watch(t, c, base+"?watch=true&resourceVersion=999999")); e.Type != "ERROR" || e.Object["code"] != 410.0 {
		t.Fatalf("watch from a resourceVersion this dennet never had: want 410, got %v", e)
	}

	if code, m := do("DELETE", base+"/build", "", ""); code != 200 {
		t.Fatalf("delete: %d %v", code, m)
	}
	if e := next(t, events); e.Type != "DELETED" {
		t.Fatalf("want DELETED, got %v", e)
	}
	if code, _ := do("PATCH", base+"/build/status", "application/merge-patch+json", patch); code != 404 {
		t.Fatalf("status patch after delete: want 404, got %d", code)
	}

	lease := `{"apiVersion":"coordination.k8s.io/v1","kind":"Lease","metadata":{"name":"build"},"spec":{"holderIdentity":"den","leaseDurationSeconds":30}}`
	if code, m := do("POST", "http://dennet/apis/coordination.k8s.io/v1/namespaces/p/leases", "application/json", lease); code != 201 {
		t.Fatalf("lease create: %d %v", code, m)
	}

	// Creating a Den made its namespace; "default" exists from the start.
	for _, n := range []string{"p", "default"} {
		if code, m := do("GET", "http://dennet/api/v1/namespaces/"+n, "", ""); code != 200 || m["status"].(map[string]any)["phase"] != "Active" {
			t.Fatalf("namespace %s: %d %v", n, code, m)
		}
	}
	if code, m := do("POST", "http://dennet/api/v1/namespaces", "application/json", `{"metadata":{"name":"q"}}`); code != 201 {
		t.Fatalf("namespace create: %d %v", code, m)
	}
}

// TestManifests checks that files in the manifests dir are applied at start
// and as they change, and that their objects go when they are dropped.
func TestManifests(t *testing.T) {
	mdir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mdir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const den = "apiVersion: den.mkm.pub/v1alpha1\nkind: Den\nmetadata: {name: d, namespace: m}\nspec: {command: [sh]}\n"
	const lease = "apiVersion: coordination.k8s.io/v1\nkind: Lease\nmetadata: {name: l}\n"
	write("a.yaml", den+"---\n"+lease)
	write(".a.yaml.swp", "not yaml")

	c, _ := start(t, mdir)
	denURL := "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/m/dens/d"
	leaseURL := "http://dennet/apis/coordination.k8s.io/v1/namespaces/default/leases/l"
	nsURL := "http://dennet/api/v1/namespaces/n"
	// Ready means the first sync is done, so no waiting here.
	for _, u := range []string{denURL, leaseURL} {
		if code := get(t, c, u); code != 200 {
			t.Fatalf("GET %s after start: %d", u, code)
		}
	}
	eventually := func(url string, want int) {
		t.Helper()
		for i := 0; get(t, c, url) != want; i++ {
			if i == 100 {
				t.Fatalf("GET %s: want %d", url, want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	write("a.yaml", lease) // drops the Den
	eventually(denURL, 404)
	write("b.json", `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"n"}}`)
	eventually(nsURL, 200)
	os.Remove(filepath.Join(mdir, "a.yaml"))
	eventually(leaseURL, 404)
	if code := get(t, c, nsURL); code != 200 {
		t.Fatalf("namespace from b.json: %d", code)
	}
}

func get(t *testing.T, c *http.Client, url string) int {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// start runs dennet with its state in a temporary dir until the test ends,
// and returns a client for its socket once it is ready.
func start(t *testing.T, manifests string) (*http.Client, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "dn") // short: Unix socket paths are limited to ~104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{StateDir: dir, Socket: sock, Manifests: manifests}) }()
	t.Cleanup(func() { cancel(); <-done })

	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	for i := 0; ; i++ {
		if code, _ := doReady(c); code == 200 {
			break
		}
		if i == 100 {
			t.Fatal("dennet did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return c, dir
}

func doReady(c *http.Client) (int, error) {
	resp, err := c.Get("http://dennet/readyz")
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

type event struct {
	Type   string
	Object map[string]any
}

func watch(t *testing.T, c *http.Client, url string) <-chan event {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	ch := make(chan event, 10)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				ch <- e
			}
		}
	}()
	return ch
}

func next(t *testing.T, ch <-chan event) event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a watch event")
		return event{}
	}
}

// fakeDen answers one history request with colors on (den's frame: u32
// length, u8 type 7, u8 flags), with text.
func fakeDen(t *testing.T, path, text string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req := make([]byte, 6)
		if _, err := io.ReadFull(conn, req); err != nil || string(req) != "\x02\x00\x00\x00\x07\x01" {
			t.Errorf("history request: %q %v", req, err)
			return
		}
		conn.Write(binary.LittleEndian.AppendUint32(nil, uint32(1+len(text))))
		conn.Write(append([]byte{7}, text...))
	}()
}
