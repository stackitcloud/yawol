package helper

import "time"

const (
	OpenstackReconcileTime   = 5 * time.Minute
	DefaultRequeueTime       = 10 * time.Millisecond
	RevisionAnnotation       = "loadbalancer.yawol.stackit.cloud/revision"
	YawolKeepalivedFile      = "/tmp/yawolKeepalivedLastSet"
	HashLabel                = "lbm-template-hash"
	LoadBalancerKind         = "LoadBalancer"
	VRRPInstanceName         = "ENVOY"
	ContainsKeepalivedMaster = "ContainsKeepalivedMaster"
)
