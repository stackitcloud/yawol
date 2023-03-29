package helper

import "time"

const (
	OpenstackReconcileTime = 5 * time.Minute
	DefaultRequeueTime     = 10 * time.Millisecond
	RevisionAnnotation     = "loadbalancer.yawol.stackit.cloud/revision"
	HashLabel              = "lbm-template-hash"
	LoadBalancerKind       = "LoadBalancer"
	VRRPInstanceName       = "ENVOY"
)
