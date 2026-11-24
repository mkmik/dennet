package server

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

func TestFollowCWD(t *testing.T) {
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Namespace: "default", Name: name} }
	space := func(name, dir string, archived bool) *denv1.Space {
		return &denv1.Space{ObjectMeta: meta(name), Spec: denv1.SpaceSpec{WorkingDir: dir, Archived: archived}}
	}
	term := func(name, space, den string) *denv1.Surface {
		return &denv1.Surface{ObjectMeta: meta(name), Spec: denv1.SurfaceSpec{Space: space, Terminal: &denv1.TerminalSurface{Den: den}}}
	}
	den := func(name, cwd string) *denv1.Den {
		return &denv1.Den{ObjectMeta: meta(name), Status: denv1.DenStatus{CWD: cwd}}
	}
	spaces := []any{
		space("one", "/home", false),     // one terminal: follows
		space("same", "/same", false),    // already there
		space("two", "/home", false),     // two terminals: left alone
		space("web", "/home", false),     // one terminal and a web tab: follows
		space("quiet", "/home", false),   // den reports no cwd
		space("archived", "/home", true), // archived: left alone
	}
	surfaces := []any{
		term("a", "one", "d1"),
		term("b", "same", "d2"),
		term("c", "two", "d3"), term("d", "two", "d4"),
		term("e", "web", "d5"),
		&denv1.Surface{ObjectMeta: meta("f"), Spec: denv1.SurfaceSpec{Space: "web", Web: &denv1.WebSurface{URL: "https://x"}}},
		term("g", "quiet", "d6"),
		term("h", "archived", "d7"),
	}
	dens := []any{
		den("d1", "/repo"), den("d2", "/same"), den("d3", "/x"), den("d4", "/y"),
		den("d5", "/w"), den("d6", ""), den("d7", "/z"),
	}
	got := map[string]string{}
	for s, dir := range followCWD(spaces, surfaces, dens) {
		got[s.Name] = dir
	}
	want := map[string]string{"one": "/repo", "web": "/w"}
	if len(got) != len(want) || got["one"] != want["one"] || got["web"] != want["web"] {
		t.Errorf("got %v, want %v", got, want)
	}
}
