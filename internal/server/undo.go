package server

import (
	"context"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// runUndo runs dennet's undo controller until ctx is cancelled: it removes
// denv1.UndoFinalizer from Spaces, Surfaces and Dens deleted grace ago, so
// they go for good (a Den once its den has stopped, see runDens). An object's
// denv1.UndoGraceAnnotation, if set, takes the place of grace.
func runUndo(ctx context.Context, socket string, grace time.Duration) {
	c := localClient(socket, denv1.SchemeGroupVersion)
	informers := map[string]cache.SharedIndexInformer{
		"spaces":   inform(c, "spaces", &denv1.Space{}),
		"surfaces": inform(c, "surfaces", &denv1.Surface{}),
		"dens":     inform(c, "dens", &denv1.Den{}),
	}
	var synced []cache.InformerSynced
	var all []cache.SharedIndexInformer
	for _, i := range informers {
		go i.Run(ctx.Done())
		synced, all = append(synced, i.HasSynced), append(all, i)
	}
	kick := kicker(all...)
	if !cache.WaitForCacheSync(ctx.Done(), synced...) {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		// Failed patches are tried again on the next change, or after this.
		wait := 30 * time.Second
		now := time.Now()
		for resource, i := range informers {
			for _, o := range i.GetStore().List() {
				m, _ := meta.Accessor(o)
				del := m.GetDeletionTimestamp()
				if del == nil || !slices.Contains(m.GetFinalizers(), denv1.UndoFinalizer) {
					continue
				}
				g := grace
				if d, err := time.ParseDuration(m.GetAnnotations()[denv1.UndoGraceAnnotation]); err == nil {
					g = d
				}
				if left := del.Add(g).Sub(now); left > 0 {
					wait = min(wait, left)
					continue
				}
				release(ctx, c, resource, m, denv1.UndoFinalizer)
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-timer.C:
		}
	}
}
