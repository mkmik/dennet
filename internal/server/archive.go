package server

import (
	"context"
	"fmt"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// runArchive runs dennet's archive controller until ctx is cancelled. For
// every Space with spec.archived it deletes the Surfaces in it and the Dens
// they point at, sets spec.archived on the Spaces under it, and sets
// status.archivedAt once no Surface is left.
func runArchive(ctx context.Context, socket string) {
	c := localClient(socket, denv1.SchemeGroupVersion)
	spaces := inform(c, "spaces", &denv1.Space{})
	surfaces := inform(c, "surfaces", &denv1.Surface{})
	kick := kicker(spaces, surfaces)
	go spaces.Run(ctx.Done())
	go surfaces.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), spaces.HasSynced, surfaces.HasSynced) {
		return
	}
	// Failed calls are tried again on the next change, or on the tick.
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		archive(ctx, c, spaces.GetStore().List(), surfaces.GetStore().List())
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-tick.C:
		}
	}
}

// archive does one pass over the cached spaces and surfaces. Objects it
// deletes may still be in the cache on the next pass; deleting them again
// is a no-op.
func archive(ctx context.Context, c *restclient.RESTClient, spaces, surfaces []any) {
	archived, left := map[string]bool{}, map[string]bool{}
	for _, o := range spaces {
		if s := o.(*denv1.Space); s.Spec.Archived {
			archived[s.Namespace+"/"+s.Name] = true
		}
	}
	for _, o := range surfaces {
		v := o.(*denv1.Surface)
		key := v.Namespace + "/" + v.Spec.Space
		if !archived[key] {
			continue
		}
		left[key] = true
		if v.Spec.Terminal != nil && remove(ctx, c, v.Namespace, "dens", v.Spec.Terminal.Den) != nil {
			continue // keep the Surface so the Den is tried again
		}
		remove(ctx, c, v.Namespace, "surfaces", v.Name)
	}
	for _, o := range spaces {
		s := o.(*denv1.Space)
		key := s.Namespace + "/" + s.Name
		switch {
		case !s.Spec.Archived && archived[s.Namespace+"/"+s.Spec.Parent]:
			patch(ctx, c, s, "", `{"spec":{"archived":true}}`)
		case s.Spec.Archived && s.Status.ArchivedAt == nil && !left[key]:
			at := time.Now().UTC().Format(time.RFC3339)
			patch(ctx, c, s, "status", `{"status":{"archivedAt":"`+at+`"}}`)
		}
	}
}

// remove deletes an object; one already gone counts as deleted.
func remove(ctx context.Context, c *restclient.RESTClient, ns, resource, name string) error {
	err := c.Delete().Namespace(ns).Resource(resource).Name(name).Do(ctx).Error()
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: archiving: deleting %s %s/%s: %v\n", resource, ns, name, err)
	}
	return err
}

func patch(ctx context.Context, c *restclient.RESTClient, s *denv1.Space, sub, body string) {
	r := c.Patch(types.MergePatchType).Namespace(s.Namespace).Resource("spaces").Name(s.Name)
	if sub != "" {
		r = r.SubResource(sub)
	}
	if err := r.Body([]byte(body)).Do(ctx).Error(); err != nil && !apierrors.IsNotFound(err) {
		fmt.Fprintf(os.Stderr, "dennet: archiving Space %s/%s: %v\n", s.Namespace, s.Name, err)
	}
}
