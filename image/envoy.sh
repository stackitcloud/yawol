#!/sbin/openrc-run

name=$RC_SVCNAME
description="envoy"
supervisor="supervise-daemon"
command="/usr/local/bin/envoy"
command_args="-c /etc/yawol/envoy.yaml"
command_user="yawol"
output_log="/var/log/yawol/envoy.log"
error_log="/var/log/yawol/envoy.log"

depend() {
	after net
}
