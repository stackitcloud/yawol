package healthz

import (
	"context"
	"fmt"
	"net/http"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

// NewHeartbeatHeathz returns a new healthz.Checker that checks the condition
// heartbeat of the LBM that are written by the yawollet. This way we can check
// if we have a healthy connection to the API-Server, both writing (updating the
// heartbeats), and reading (retrieving the updated heartbeats), without
// additional requests to the API Server.
func NewHeartbeatHeathz(ctx context.Context,
	reader client.Reader,
	expiration time.Duration,
	namespace, loadBalancerMachineName string) healthz.Checker {
	var (
		lbm    = &yawolv1beta1.LoadBalancerMachine{}
		lbmKey = client.ObjectKey{
			Name:      loadBalancerMachineName,
			Namespace: namespace,
		}
	)
	return func(_ *http.Request) error {
		if err := reader.Get(ctx, lbmKey, lbm); err != nil {
			return fmt.Errorf("failed getting LoadBalancerMachine %q", lbmKey)
		}
		expiration := metav1.Time{Time: time.Now().Add(-expiration)}

		ok, reason := helper.AreRelevantConditionsMet(lbm, expiration, false)
		if !ok {
			return fmt.Errorf("not all relevant conditions are met, reason: %q", reason)
		}

		return nil
	}
}
