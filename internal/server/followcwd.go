package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// runFollowCWD runs dennet's follow-cwd controller until ctx is cancelled.
// While a Space has exactly one terminal Surface, it sets the Space's
// spec.workingDir to the status.cwd that den reports for that terminal's Den.
func runFollowCWD(ctx context.Context, socket string) {
	c := localClient(socket, denv1.SchemeGroupVersion)
	spaces := inform(c, "spaces", &denv1.Space{})
	surfaces := inform(c, "surfaces", &denv1.Surface{})
	dens := inform(c, "dens", &denv1.Den{})
	kick := kicker(spaces, surfaces, dens)
	go spaces.Run(ctx.Done())
	go surfaces.Run(ctx.Done())
	go dens.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), spaces.HasSynced, surfaces.HasSynced, dens.HasSynced) {
		return
	}
	// A failed patch is tried again on the next change; Den status changes often.
	for {
		for s, dir := range followCWD(spaces.GetStore().List(), surfaces.GetStore().List(), dens.GetStore().List()) {
			body, _ := json.Marshal(map[string]any{"spec": map[string]string{"workingDir": dir}})
			err := c.Patch(types.MergePatchType).Namespace(s.Namespace).Resource("spaces").Name(s.Name).Body(body).Do(ctx).Error()
			if err != nil && !apierrors.IsNotFound(err) {
				fmt.Fprintf(os.Stderr, "dennet: following cwd of Space %s/%s: %v\n", s.Namespace, s.Name, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-kick:
		}
	}
}

// followCWD returns the Spaces whose spec.workingDir should change, and to
// what: those with exactly one terminal Surface whose Den reports a cwd
// other than the Space's.
func followCWD(spaces, surfaces, dens []any) map[*denv1.Space]string {
	terminals := map[string][]string{} // Space -> Dens of its terminal Surfaces
	for _, o := range surfaces {
		if v := o.(*denv1.Surface); v.Spec.Terminal != nil {
			key := v.Namespace + "/" + v.Spec.Space
			terminals[key] = append(terminals[key], v.Spec.Terminal.Den)
		}
	}
	cwd := map[string]string{}
	for _, o := range dens {
		d := o.(*denv1.Den)
		cwd[d.Namespace+"/"+d.Name] = d.Status.CWD
	}
	out := map[*denv1.Space]string{}
	for _, o := range spaces {
		s := o.(*denv1.Space)
		ds := terminals[s.Namespace+"/"+s.Name]
		if s.Spec.Archived || len(ds) != 1 {
			continue
		}
		// Empty when the shell doesn't report its directory (OSC 7).
		if dir := cwd[s.Namespace+"/"+ds[0]]; strings.HasPrefix(dir, "/") && dir != s.Spec.WorkingDir {
			out[s] = dir
		}
	}
	return out
}
