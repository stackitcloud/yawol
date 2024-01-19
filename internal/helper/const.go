package helper

import "time"

const (
	OpenstackReconcileTime    = 5 * time.Minute
	DefaultRequeueTime        = 10 * time.Millisecond
	RevisionAnnotation        = "loadbalancer.yawol.stackit.cloud/revision"
	YawolLibDir               = "/var/lib/yawol/"
	YawolletHealthCheckScript = YawolLibDir + "check_yawollet_health.sh"
	HashLabel                 = "lbm-template-hash"
	LoadBalancerKind          = "LoadBalancer"
	VRRPInstanceName          = "ENVOY"
	HasKeepalivedMaster       = "HasKeepalivedMaster"
	DefaultLoadbalancerClass  = "stackit.cloud/yawol"
)
