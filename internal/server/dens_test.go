package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// TestDens checks that creating a Den runs den apply with the Den's URL on
// dennet's socket, and that deleting it shuts the den down before the Den
// and its Lease go away.
func TestDens(t *testing.T) {
	dir := tempDir(t)
	log := filepath.Join(dir, "spawned")
	den := filepath.Join(dir, "den")
	os.WriteFile(den, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\necho /fake.sock\n"), 0o700)
	c, _ := run(t, Options{StateDir: dir, Den: den})
	do := func(method, url, ctype, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/dens"
	const leaseURL = "http://dennet/apis/coordination.k8s.io/v1/namespaces/p/leases/a"
	eventually := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; !ok(); i++ {
			if i == 100 {
				t.Fatal(what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	_, d := do("POST", base, "application/json", `{"apiVersion":"den.mkm.pub/v1alpha1","kind":"Den","metadata":{"name":"a"}}`)
	uid := d["metadata"].(map[string]any)["uid"].(string)
	want := "apply -f " + base + "/a --unix-socket " + filepath.Join(dir, "s") + "\n"
	eventually("den apply did not run", func() bool { got, _ := os.ReadFile(log); return string(got) == want })

	// Play the den: a socket that answers shutdown by closing, and a Lease.
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	shutdown := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		frame := make([]byte, 5)
		io.ReadFull(conn, frame)
		ln.Close()
		conn.Close()
		shutdown <- frame
	}()
	do("PATCH", base+"/a/status", "application/merge-patch+json", `{"status":{"phase":"Running","socket":"`+sock+`"}}`)
	lease := `{"apiVersion":"coordination.k8s.io/v1","kind":"Lease","metadata":{"name":"a","ownerReferences":[{"apiVersion":"den.mkm.pub/v1alpha1","kind":"Den","name":"a","uid":"` +
		uid + `"}]},"spec":{"holderIdentity":"den","leaseDurationSeconds":30}}`
	if code, m := do("POST", "http://dennet/apis/coordination.k8s.io/v1/namespaces/p/leases", "application/json", lease); code != 201 {
		t.Fatalf("lease create: %d %v", code, m)
	}

	if code, m := do("DELETE", base+"/a", "", ""); code != 200 {
		t.Fatalf("delete: %d %v", code, m)
	}
	select {
	case f := <-shutdown:
		if string(f) != "\x01\x00\x00\x00\x0b" {
			t.Fatalf("want a shutdown frame, got %q", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("den was not asked to shut down")
	}
	eventually("Den or Lease still there", func() bool {
		code, _ := do("GET", base+"/a", "", "")
		lcode, _ := do("GET", leaseURL, "", "")
		return code == 404 && lcode == 404
	})
}

func TestNeedsDen(t *testing.T) {
	since := time.Unix(1000, 0)
	now := since.Add(time.Minute)
	den := func(phase denv1.DenPhase) *denv1.Den {
		return &denv1.Den{ObjectMeta: metav1.ObjectMeta{UID: "u"}, Status: denv1.DenStatus{Phase: phase}}
	}
	lease := func(renewed time.Time, owner types.UID) *coordinationv1.Lease {
		return &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{UID: owner}}},
			Spec:       coordinationv1.LeaseSpec{RenewTime: &metav1.MicroTime{Time: renewed}, LeaseDurationSeconds: ptr.To[int32](30)},
		}
	}
	deleting := den(denv1.DenRunning)
	deleting.DeletionTimestamp = &metav1.Time{Time: since}
	elsewhere := den("")
	elsewhere.Spec.HostName = "box"
	for _, c := range []struct {
		name string
		d    *denv1.Den
		l    *coordinationv1.Lease
		want bool
	}{
		{"new", den(""), nil, true},
		{"live lease", den(denv1.DenRunning), lease(now.Add(-10*time.Second), "u"), false},
		{"expired lease", den(denv1.DenRunning), lease(now.Add(-31*time.Second), "u"), true},
		{"earlier Den's lease", den(""), lease(now, "other"), true},
		{"just restarted", den(denv1.DenRunning), lease(since.Add(-time.Hour), "u"), false},
		{"exited", den(denv1.DenExited), nil, false},
		{"failed", den(denv1.DenFailed), nil, false},
		{"deleting", deleting, nil, false},
		{"other host", elsewhere, nil, false},
	} {
		n := now
		if c.name == "just restarted" {
			n = since.Add(10 * time.Second)
		}
		if got := needsDen(c.d, c.l, since, n); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
