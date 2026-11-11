package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestArchive checks that archiving a Space deletes its Surfaces and their
// Dens, archives the Space under it, and sets status.archivedAt; and that
// reopening clears archivedAt.
func TestArchive(t *testing.T) {
	c, _ := start(t)
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/"
	do := func(method, url, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, base+url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if method == "PATCH" {
			req.Header.Set("Content-Type", "application/merge-patch+json")
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
	eventually := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; !ok(); i++ {
			if i == 100 {
				t.Fatal(what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	archivedAt := func(name string) any {
		_, m := do("GET", "spaces/"+name, "")
		st, _ := m["status"].(map[string]any)
		return st["archivedAt"]
	}
	for _, o := range []struct{ res, body string }{
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"p"}}`},
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"c"},"spec":{"parent":"p"}}`},
		{"spaces", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"other"}}`},
		{"dens", `{"kind":"Den","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"d"}}`},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"s"},"spec":{"space":"p","kind":"terminal","terminal":{"den":"d"}}}`},
		{"surfaces", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"keep"},"spec":{"space":"other","kind":"terminal","terminal":{"den":"e"}}}`},
	} {
		if code, m := do("POST", o.res, o.body); code != 201 {
			t.Fatalf("POST %s: %d %v", o.res, code, m)
		}
	}
	// status is dennet's: a write on the Space itself does not set it.
	do("PATCH", "spaces/p", `{"status":{"archivedAt":"2020-01-01T00:00:00Z"}}`)
	if at := archivedAt("p"); at != nil {
		t.Fatalf("status written through spaces: %v", at)
	}

	do("PATCH", "spaces/p", `{"spec":{"archived":true}}`)
	eventually("p not archived", func() bool { return archivedAt("p") != nil })
	if code, _ := do("GET", "surfaces/s", ""); code != 404 {
		t.Errorf("surface: got %d, want 404", code)
	}
	// The Den goes once dennet has stopped its den (see TestDens).
	eventually("den not deleted", func() bool { code, _ := do("GET", "dens/d", ""); return code == 404 })
	if code, _ := do("GET", "surfaces/keep", ""); code != 200 {
		t.Errorf("surface of another space: got %d, want 200", code)
	}
	eventually("child not archived", func() bool { return archivedAt("c") != nil })

	do("PATCH", "spaces/p", `{"spec":{"archived":false}}`)
	if at := archivedAt("p"); at != nil {
		t.Errorf("reopened space keeps archivedAt %v", at)
	}
	if _, m := do("GET", "spaces/c", ""); m["spec"].(map[string]any)["archived"] != true {
		t.Errorf("reopening p reopened c")
	}
}
