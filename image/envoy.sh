#!/sbin/openrc-run

name=$RC_SVCNAME
description="envoy"
command="/usr/local/bin/envoy"
command_args="-c /etc/yawol/envoy.yaml"
command_user="yawol"
command_background="yes"
output_logger="logger -t envoy"
error_logger="logger -t envoy"
pidfile="/run/$RC_SVCNAME.pid"

depend() {
	after net
}
