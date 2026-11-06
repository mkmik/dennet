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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// TestDenProtocol drives dennet over its Unix socket with plain HTTP, the way
// den does: create, single-object watch, status PATCH with a uid
// precondition, stale watches and deletion.
func TestDenProtocol(t *testing.T) {
	c, dir := start(t)
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

	// Once etcd compacts past a resourceVersion, watching from it is 410.
	_, cur := do("GET", base+"/build", "", "")
	compact(t, dir, cur["metadata"].(map[string]any)["resourceVersion"].(string))
	if e := next(t, watch(t, c, base+"?watch=true&resourceVersion="+rv)); e.Type != "ERROR" || e.Object["code"] != 410.0 {
		t.Fatalf("watch from a compacted resourceVersion: want 410, got %v", e)
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

// TestSpacesAndSurfaces checks that Spaces and Surfaces are served and that bad
// specs are rejected.
func TestSpacesAndSurfaces(t *testing.T) {
	c, _ := start(t)
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/"
	post := func(res, body string) int {
		t.Helper()
		resp, err := c.Post(base+res, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, tc := range []struct {
		res, body string
		want      int
	}{
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"kubecfg"},"spec":{"title":"kubecfg"}}`, 201},
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"m"},"spec":{"parent":"kubecfg","order":20,"state":"waiting","badges":[{"text":"MR !412","tone":"info"}]}}`, 201},
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"state":"sleepy"}}`, 422},
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"parent":"x"}}`, 422},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"m-claude"},"spec":{"space":"m","kind":"terminal","terminal":{"den":"m-claude"}}}`, 201},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"space":"m","kind":"terminal"}}`, 422},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"space":"m","kind":"browser"}}`, 422},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"x"},"spec":{"kind":"terminal","terminal":{"den":"d"}}}`, 422},
	} {
		if got := post(tc.res, tc.body); got != tc.want {
			t.Errorf("POST %s %s: got %d, want %d", tc.res, tc.body, got, tc.want)
		}
	}
	if code := get(t, c, base+"surfaces/m-claude"); code != 200 {
		t.Fatalf("GET surface: %d", code)
	}
	req, _ := http.NewRequest("GET", base+"spaces/kubecfg", nil)
	req.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tbl struct {
		ColumnDefinitions []struct{ Name string }
		Rows              []struct{ Cells []any }
	}
	json.NewDecoder(resp.Body).Decode(&tbl)
	if len(tbl.Rows) != 1 || len(tbl.ColumnDefinitions) < 2 || tbl.ColumnDefinitions[1].Name != "Title" || tbl.Rows[0].Cells[1] != "kubecfg" {
		t.Fatalf("space table should have a Title column: %+v", tbl)
	}
}

// TestRestart checks that objects outlive a restart on the same state dir.
func TestRestart(t *testing.T) {
	dir := tempDir(t)
	c, stop := run(t, dir)
	const url = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/spaces"
	resp, err := c.Post(url, "application/json", strings.NewReader(`{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"kept"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	stop()

	c, _ = run(t, dir)
	if code := get(t, c, url+"/kept"); code != 200 {
		t.Fatalf("GET after restart: %d", code)
	}
}

// compact compacts dennet's etcd up to resourceVersion rv.
func compact(t *testing.T, dir, rv string) {
	t.Helper()
	cli, err := clientv3.New(clientv3.Config{Endpoints: []string{"unix://" + filepath.Join(dir, "etcd.sock")}})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	n, _ := strconv.ParseInt(rv, 10, 64)
	if _, err := cli.Compact(context.Background(), n); err != nil {
		t.Fatal(err)
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
func start(t *testing.T) (*http.Client, string) {
	dir := tempDir(t)
	c, _ := run(t, dir)
	return c, dir
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dn") // short: Unix socket paths are limited to ~104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// run runs dennet on dir until stop is called or the test ends.
func run(t *testing.T, dir string) (c *http.Client, stop func()) {
	t.Helper()
	sock := filepath.Join(dir, "s")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{StateDir: dir, Socket: sock}) }()
	stop = sync.OnceFunc(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	t.Cleanup(stop)

	c = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	for i := 0; ; i++ {
		if code, _ := doReady(c); code == 200 {
			break
		}
		if i == 200 {
			t.Fatal("dennet did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return c, stop
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

// TestHost checks that a namespace named like a Host is the default
// namespace of the dennet on that host: writes, reads and watches go there,
// and come back with the Host's namespace.
func TestHost(t *testing.T) {
	far, farDir := start(t)
	here, _ := start(t)
	ssh := dialHost
	t.Cleanup(func() { dialHost = ssh })
	dialHost = func(spec denv1.HostSpec) (net.Conn, error) {
		if spec.SSH != "far.example" {
			t.Errorf("dialled %q", spec.SSH)
		}
		return net.Dial("unix", filepath.Join(farDir, "s"))
	}

	post := func(c *http.Client, url, body string) (int, map[string]any) {
		t.Helper()
		resp, err := c.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	const hosts = "http://dennet/apis/den.mkm.pub/v1alpha1/hosts"
	if code, m := post(here, hosts, `{"kind":"Host","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"default"},"spec":{"ssh":"x"}}`); code != 422 {
		t.Fatalf("Host named default: want 422, got %d %v", code, m)
	}
	if code, m := post(here, hosts, `{"kind":"Host","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"far"},"spec":{"ssh":"-oProxyCommand=x"}}`); code != 422 {
		t.Fatalf("ssh option as destination: want 422, got %d %v", code, m)
	}
	if code, m := post(here, hosts, `{"kind":"Host","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"far"},"spec":{"ssh":"far.example"}}`); code != 201 {
		t.Fatalf("Host: %d %v", code, m)
	}

	const viaHere = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/far/spaces"
	const onFar = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/spaces"
	nsOf := func(m map[string]any) any { return m["metadata"].(map[string]any)["namespace"] }

	code, m := post(here, viaHere, `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"a","namespace":"far"}}`)
	if code != 201 || nsOf(m) != "far" {
		t.Fatalf("create through Host: %d %v", code, m)
	}
	if code := get(t, far, onFar+"/a"); code != 200 {
		t.Fatalf("on far, default/a: %d", code)
	}
	if code := get(t, here, "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/spaces/a"); code != 404 {
		t.Fatalf("here, default/a: want 404, got %d", code)
	}

	events := watch(t, here, viaHere+"?watch=true")
	if e := next(t, events); e.Type != "ADDED" || nsOf(e.Object) != "far" {
		t.Fatalf("watch through Host: %v", e)
	}
	post(far, onFar, `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"b"}}`)
	if e := next(t, events); e.Type != "ADDED" || e.Object["metadata"].(map[string]any)["name"] != "b" || nsOf(e.Object) != "far" {
		t.Fatalf("watch through Host: %v", e)
	}
}
