#!/sbin/openrc-run

name=$RC_SVCNAME
description="envoy"
supervisor="supervise-daemon"
command="/usr/local/bin/envoy"
command_args="-c /etc/yawol/envoy.yaml --log-path /var/log/yawol/envoy.log"
command_user="yawol"

depend() {
	after net 
}