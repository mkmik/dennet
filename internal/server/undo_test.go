package server

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// TestUndo checks that a deleted Space, Surface or Den stays, marked, until
// the undo grace is over, keeping its den running; that creating it again
// brings it back with the same uid and the new spec; and that
// gracePeriodSeconds=0 deletes right away.
func TestUndo(t *testing.T) {
	dir := tempDir(t)
	c, _ := run(t, Options{StateDir: dir, UndoGrace: time.Hour})
	const base = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/"
	objects := []struct{ res, name, body string }{
		{"spaces", "s", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"s"},"spec":{"title":"%s"}}`},
		{"dens", "d", `{"kind":"Den","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"d"},"spec":{"workingDir":"/%s"}}`},
		{"surfaces", "v", `{"kind":"Surface","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"v","labels":{"l":"%s"}},"spec":{"space":"s","kind":"terminal","terminal":{"den":"d"}}}`},
	}
	uids := map[string]any{}
	for _, o := range objects {
		code, m := do(t, c, "POST", base+o.res, "application/json", fmt.Sprintf(o.body, "old"))
		if code != 201 {
			t.Fatalf("create %s: %d %v", o.res, code, m)
		}
		md := m["metadata"].(map[string]any)
		if !slices.Contains(md["finalizers"].([]any), any(denv1.UndoFinalizer)) {
			t.Fatalf("%s has no undo finalizer: %v", o.res, md)
		}
		uids[o.res] = md["uid"]
	}
	sock := filepath.Join(dir, "d.sock")
	_, shutdown := fakeDen(t, sock, "")
	if code, m := do(t, c, "PATCH", base+"dens/d/status", "application/merge-patch+json", `{"status":{"phase":"Running","socket":"`+sock+`"}}`); code != 200 {
		t.Fatalf("status patch: %d %v", code, m)
	}

	for _, o := range objects {
		if code, m := do(t, c, "DELETE", base+o.res+"/"+o.name, "", ""); code != 200 {
			t.Fatalf("delete %s: %d %v", o.res, code, m)
		}
		if code, m := do(t, c, "GET", base+o.res+"/"+o.name, "", ""); code != 200 || m["metadata"].(map[string]any)["deletionTimestamp"] == nil {
			t.Fatalf("deleted %s should stay, marked: %d %v", o.res, code, m)
		}
	}
	select {
	case <-shutdown:
		t.Fatal("den shut down while its Den can still be brought back")
	case <-time.After(500 * time.Millisecond):
	}

	for _, o := range objects {
		code, m := do(t, c, "POST", base+o.res, "application/json", fmt.Sprintf(o.body, "new"))
		if code != 201 {
			t.Fatalf("create %s again: %d %v", o.res, code, m)
		}
		md := m["metadata"].(map[string]any)
		if md["uid"] != uids[o.res] || md["deletionTimestamp"] != nil {
			t.Fatalf("%s not brought back in place: %v", o.res, md)
		}
	}
	_, s := do(t, c, "GET", base+"spaces/s", "", "")
	_, d := do(t, c, "GET", base+"dens/d", "", "")
	_, v := do(t, c, "GET", base+"surfaces/v", "", "")
	if s["spec"].(map[string]any)["title"] != "new" || d["spec"].(map[string]any)["workingDir"] != "/new" ||
		v["metadata"].(map[string]any)["labels"].(map[string]any)["l"] != "new" {
		t.Fatalf("brought back without the new spec: %v %v %v", s, d, v)
	}
	// Deleting bumped the generation too.
	if d["status"].(map[string]any)["phase"] != "Running" || d["metadata"].(map[string]any)["generation"] != 3.0 {
		t.Fatalf("Den brought back should keep status and bump generation: %v", d)
	}
	// Not deleted: a name in use is still a conflict.
	if code, _ := do(t, c, "POST", base+"spaces", "application/json", fmt.Sprintf(objects[0].body, "x")); code != 409 {
		t.Fatalf("create over a live Space: want 409, got %d", code)
	}

	for _, o := range objects {
		if code, m := do(t, c, "DELETE", base+o.res+"/"+o.name+"?gracePeriodSeconds=0", "", ""); code != 200 {
			t.Fatalf("delete %s now: %d %v", o.res, code, m)
		}
	}
	select {
	case <-shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("den was not asked to shut down")
	}
	for _, o := range objects {
		eventually(t, o.res+" still there", func() bool { code, _ := do(t, c, "GET", base+o.res+"/"+o.name, "", ""); return code == 404 })
	}
}

// TestUndoGrace checks that a deleted object goes once the grace is over,
// and is then made anew.
func TestUndoGrace(t *testing.T) {
	c, _ := run(t, Options{StateDir: tempDir(t), UndoGrace: time.Second})
	const url = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/spaces"
	const body = `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"s"}}`
	_, m := do(t, c, "POST", url, "application/json", body)
	uid := m["metadata"].(map[string]any)["uid"]
	do(t, c, "DELETE", url+"/s", "", "")
	eventually(t, "Space still there", func() bool { code, _ := do(t, c, "GET", url+"/s", "", ""); return code == 404 })
	if _, m := do(t, c, "POST", url, "application/json", body); m["metadata"].(map[string]any)["uid"] == uid {
		t.Fatal("made again after the grace, but with the old uid")
	}
}

// TestUndoGraceAnnotation checks that denv1.UndoGraceAnnotation=0s (or a
// bare 0) skips the undo wait, whether set before the delete or while the
// object waits.
func TestUndoGraceAnnotation(t *testing.T) {
	c, _ := run(t, Options{StateDir: tempDir(t), UndoGrace: time.Hour})
	const url = "http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/p/spaces"
	do(t, c, "POST", url, "application/json", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"a","annotations":{"`+denv1.UndoGraceAnnotation+`":"0"}}}`)
	do(t, c, "POST", url, "application/json", `{"kind":"Space","apiVersion":"den.mkm.pub/v1alpha1","metadata":{"name":"b"}}`)
	do(t, c, "DELETE", url+"/a", "", "")
	do(t, c, "DELETE", url+"/b", "", "")
	eventually(t, "annotated Space still there", func() bool { code, _ := do(t, c, "GET", url+"/a", "", ""); return code == 404 })
	if code, _ := do(t, c, "GET", url+"/b", "", ""); code != 200 {
		t.Fatalf("Space without the annotation should wait: %d", code)
	}
	if code, m := do(t, c, "PATCH", url+"/b", "application/merge-patch+json", `{"metadata":{"annotations":{"`+denv1.UndoGraceAnnotation+`":"0s"}}}`); code != 200 {
		t.Fatalf("annotate waiting Space: %d %v", code, m)
	}
	eventually(t, "Space annotated while waiting still there", func() bool { code, _ := do(t, c, "GET", url+"/b", "", ""); return code == 404 })
}
