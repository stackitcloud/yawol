package kubernetes

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// AddFinalizerIfNeeded adds a finalizer via patch if is not present in the object
func AddFinalizerIfNeeded(ctx context.Context, c client.Client, object client.Object, finalizer string) error {
	if controllerutil.ContainsFinalizer(object, finalizer) {
		return nil
	}

	finalizersJSON, err := json.Marshal(append(object.GetFinalizers(), finalizer))
	if err != nil {
		return err
	}

	patch := []byte(`{"metadata":{"finalizers": ` + string(finalizersJSON) + ` }}`)
	return c.Patch(ctx, object, client.RawPatch(types.MergePatchType, patch))
}

// RemoveFinalizerIfNeeded removes a finalizer via patch if is present in the object
func RemoveFinalizerIfNeeded(ctx context.Context, c client.Client, object client.Object, finalizer string) error {
	if !controllerutil.ContainsFinalizer(object, finalizer) {
		return nil
	}

	controllerutil.RemoveFinalizer(object, finalizer)

	finalizers, err := json.Marshal(object.GetFinalizers())
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata":{"finalizers": ` + string(finalizers) + ` }}`)
	return c.Patch(ctx, object, client.RawPatch(types.MergePatchType, patch))
}
