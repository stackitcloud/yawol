package charts

import (
	"embed"
)

var (
	//go:embed all:yawol-controller
	YawolController     embed.FS
	YawolControllerPath = "yawol-controller"
)
