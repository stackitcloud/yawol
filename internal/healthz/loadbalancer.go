package healthz

import (
	"context"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
)

// NewLoadBalancerRevisionHealthz returns a new healthz.Checker that will pass if the LoadBalancerMachine belongs to
// the latest revision of the corresponding LoadBalancer.
// TODO: unit tests
func NewLoadBalancerRevisionHealthz(
	ctx context.Context,
	reader client.Reader,
	namespace, loadBalancerName, loadBalancerMachineName string,
) healthz.Checker {
	var (
		lb    = &yawolv1beta1.LoadBalancer{}
		lbKey = client.ObjectKey{
			Name:      loadBalancerName,
			Namespace: namespace,
		}

		lbm    = &yawolv1beta1.LoadBalancerMachine{}
		lbmKey = client.ObjectKey{
			Name:      loadBalancerMachineName,
			Namespace: namespace,
		}
	)

	return func(_ *http.Request) error {
		if err := reader.Get(ctx, client.ObjectKeyFromObject(lb), lb); err != nil {
			return fmt.Errorf("failed getting LoadBalancer %q", lbKey)
		}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(lbm), lbm); err != nil {
			return fmt.Errorf("failed getting LoadBalancerMachine %q", lbmKey)
		}

		lbRevision := lb.Annotations[helper.RevisionAnnotation]
		lbmRevision := lbm.Annotations[helper.RevisionAnnotation]
		if lbRevision != lbmRevision {
			return fmt.Errorf("LoadBalancer revision %s does not match LoadBalancerMachine revision %s", lbRevision, lbmRevision)
		}

		return nil
	}
}
