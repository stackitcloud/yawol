package healthz

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

// NewAPIServerHealthz returns a new healthz.Checker that will pass only if the /healthz endpoint of the API server
// returns status code 200.
func NewAPIServerHealthz(ctx context.Context, restClient rest.Interface) healthz.Checker {
	// TODO: This check is evil! Reading from the API server every 2 seconds from every LoadBalancerMachine generates an
	// immense load when running many LoadBalancers.
	// Find a better way to determine whether we can talk to the API server without DDoSing it.

	return func(_ *http.Request) error {
		result := restClient.Get().AbsPath("/healthz").Do(ctx)
		if err := result.Error(); err != nil {
			return err
		}

		var statusCode int
		result.StatusCode(&statusCode)
		if statusCode != http.StatusOK {
			return fmt.Errorf("failed talking to the cluster's kube-apiserver (status code: %d)", statusCode)
		}
		return nil
	}
}
