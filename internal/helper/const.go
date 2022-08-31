package helper

import "time"

const (
	DefaultRequeueTime = 10 * time.Millisecond
	RevisionAnnotation = "loadbalancer.yawol.stackit.cloud/revision"
	HashLabel          = "lbm-template-hash"
	LoadBalancerKind   = "LoadBalancer"
	VRRPInstanceName   = "ENVOY"
)
