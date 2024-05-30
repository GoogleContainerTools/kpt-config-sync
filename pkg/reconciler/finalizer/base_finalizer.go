package finalizer

import (
	"context"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/status"
)

type baseFinalizer struct {
	Destroyer applier.Destroyer
}

// destroy calls Destroyer.Destroy, collects errors, and handles logging,
// similar to Updater.apply.
func (bf *baseFinalizer) destroy(ctx context.Context) status.MultiError {
	var err status.MultiError
	superEventHandler := func(event applier.SuperEvent) {
		if errEvent, ok := event.(applier.SuperErrorEvent); ok {
			if err == nil {
				err = errEvent.Error
			} else {
				err = status.Append(err, errEvent.Error)
			}
		}
	}
	klog.V(1).Info("Destroyer starting...")
	// start := time.Now()
	objStatusMap, syncStats := bf.Destroyer.Destroy(ctx, superEventHandler)
	if syncStats.Empty() {
		klog.V(4).Info("Destroyer made no new progress")
	} else {
		klog.Infof("Destroyer made new progress: %s", syncStats.String())
		objStatusMap.Log(klog.V(0))
	}
	// TODO: should we have a destroy duration metric?
	// metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(errs), commit, start)
	if err != nil {
		klog.Warningf("Failed to destroy declared resources: %v", err)
		return err
	}
	klog.V(4).Info("Destroyer completed without error: all resources are deleted.")
	klog.V(3).Info("Applier stopped")
	return nil
}
