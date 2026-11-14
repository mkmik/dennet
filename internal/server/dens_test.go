package server

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

const (
	densURL   = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/dens"
	leasesURL = "http://dennet/apis/coordination.k8s.io/v1/namespaces/p/leases"
)

// do sends a request with body (of type ctype) and decodes the JSON reply.
func do(t *testing.T, c *http.Client, method, url, ctype, body string) (int, map[string]any) {
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

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i == 100 {
			t.Fatal(what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func createDen(t *testing.T, c *http.Client, name string) {
	t.Helper()
	if code, m := do(t, c, "POST", densURL, "application/json", `{"apiVersion":"den.mkm.pub/v1alpha1","kind":"Den","metadata":{"name":"`+name+`"}}`); code != 201 {
		t.Fatalf("create Den %s: %d %v", name, code, m)
	}
}

func setSocket(t *testing.T, c *http.Client, name, sock string) {
	t.Helper()
	if code, m := do(t, c, "PATCH", densURL+"/"+name+"/status", "application/merge-patch+json", `{"status":{"phase":"Running","socket":"`+sock+`"}}`); code != 200 {
		t.Fatalf("status patch: %d %v", code, m)
	}
}

// TestDens checks that creating a Den runs den apply with the Den's URL on
// dennet's socket, and that deleting it shuts the den down before the Den
// and its Lease go away.
func TestDens(t *testing.T) {
	dir := tempDir(t)
	log := filepath.Join(dir, "spawned")
	den := filepath.Join(dir, "den")
	os.WriteFile(den, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\necho /fake.sock\n"), 0o700)
	c, _ := run(t, Options{StateDir: dir, Den: den})

	createDen(t, c, "a")
	want := "apply -f " + densURL + "/a --unix-socket " + filepath.Join(dir, "s") + "\n"
	eventually(t, "den apply did not run", func() bool { got, _ := os.ReadFile(log); return string(got) == want })

	sock := filepath.Join(dir, "a.sock")
	_, shutdown := fakeDen(t, sock, "")
	setSocket(t, c, "a", sock)
	eventually(t, "no Lease for a running den", func() bool { code, _ := do(t, c, "GET", leasesURL+"/a", "", ""); return code == 200 })

	if code, m := do(t, c, "DELETE", densURL+"/a", "", ""); code != 200 {
		t.Fatalf("delete: %d %v", code, m)
	}
	select {
	case <-shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("den was not asked to shut down")
	}
	eventually(t, "Den or Lease still there", func() bool {
		code, _ := do(t, c, "GET", densURL+"/a", "", "")
		lcode, _ := do(t, c, "GET", leasesURL+"/a", "", "")
		return code == 404 && lcode == 404
	})
}

// TestLeases checks that a Den has a Lease while its den's socket is open,
// and none once it closes or when only a stale socket file is left; and
// that Leases can't be written.
func TestLeases(t *testing.T) {
	c, dir := start(t)
	createDen(t, c, "a")
	_, list := do(t, c, "GET", leasesURL, "", "")
	rv := list["metadata"].(map[string]any)["resourceVersion"].(string)
	events := watch(t, c, leasesURL+"?watch=true&resourceVersion="+rv)

	sock := filepath.Join(dir, "a.sock")
	stop, _ := fakeDen(t, sock, "")
	setSocket(t, c, "a", sock)
	if e := next(t, events); e.Type != "ADDED" || e.Object["metadata"].(map[string]any)["name"] != "a" {
		t.Fatalf("want ADDED a, got %v", e)
	}
	if code, m := do(t, c, "GET", leasesURL+"/a", "", ""); code != 200 || m["spec"].(map[string]any)["renewTime"] == nil {
		t.Fatalf("GET lease: %d %v", code, m)
	}
	if _, m := do(t, c, "GET", leasesURL, "", ""); len(m["items"].([]any)) != 1 {
		t.Fatalf("list: %v", m)
	}

	stop()
	if e := next(t, events); e.Type != "DELETED" {
		t.Fatalf("want DELETED, got %v", e)
	}
	if code, _ := do(t, c, "GET", leasesURL+"/a", "", ""); code != 404 {
		t.Fatalf("GET lease of a closed den: want 404, got %d", code)
	}

	// A crashed den leaves its socket file behind.
	stale := filepath.Join(dir, "b.sock")
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	createDen(t, c, "b")
	setSocket(t, c, "b", stale)
	if code, _ := do(t, c, "GET", leasesURL+"/b", "", ""); code != 404 {
		t.Fatalf("GET lease of a stale socket: want 404, got %d", code)
	}

	// No patch verb, so den doesn't renew; a den that tries gets 405.
	_, disc := do(t, c, "GET", "http://dennet/apis/coordination.k8s.io/v1", "", "")
	for _, r := range disc["resources"].([]any) {
		r := r.(map[string]any)
		if r["name"] != "leases" {
			continue
		}
		if verbs := r["verbs"].([]any); slices.Contains(verbs, any("patch")) || !slices.Contains(verbs, any("watch")) {
			t.Fatalf("lease verbs: %v", verbs)
		}
	}
	apply := `{"apiVersion":"coordination.k8s.io/v1","kind":"Lease","metadata":{"name":"a"},"spec":{"leaseDurationSeconds":30}}`
	if code, _ := do(t, c, "PATCH", leasesURL+"/a?fieldManager=den", "application/apply-patch+yaml", apply); code != 405 {
		t.Fatalf("apply: want 405, got %d", code)
	}
}

// TestLeasesRestart checks that after a restart the Lease of a den that kept
// running is there on the first read, and no second den is started.
func TestLeasesRestart(t *testing.T) {
	dir := tempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir) // where DenSocket looks without status.socket
	log := filepath.Join(dir, "spawned")
	den := filepath.Join(dir, "fake-den")
	os.WriteFile(den, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o700)
	fakeDen(t, filepath.Join(dir, "den", "p", "a.sock"), "")

	c, stop := run(t, Options{StateDir: dir, Den: den})
	createDen(t, c, "a")
	eventually(t, "no Lease", func() bool { code, _ := do(t, c, "GET", leasesURL+"/a", "", ""); return code == 200 })
	stop()

	c, _ = run(t, Options{StateDir: dir, Den: den})
	if _, m := do(t, c, "GET", leasesURL, "", ""); len(m["items"].([]any)) != 1 {
		t.Fatalf("first list after restart: %v", m)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(log); err == nil {
		t.Fatal("started a den for a den that is running")
	}
}

func TestNeedsDen(t *testing.T) {
	den := func(phase denv1.DenPhase) *denv1.Den {
		return &denv1.Den{Status: denv1.DenStatus{Phase: phase}}
	}
	deleting := den(denv1.DenRunning)
	deleting.DeletionTimestamp = &metav1.Time{}
	elsewhere := den("")
	elsewhere.Spec.HostName = "box"
	for _, c := range []struct {
		name string
		d    *denv1.Den
		held bool
		want bool
	}{
		{"new", den(""), false, true},
		{"running", den(denv1.DenRunning), true, false},
		{"gone", den(denv1.DenRunning), false, true},
		{"exited", den(denv1.DenExited), false, false},
		{"failed", den(denv1.DenFailed), false, false},
		{"deleting", deleting, false, false},
		{"other host", elsewhere, false, false},
	} {
		if got := needsDen(c.d, c.held); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
