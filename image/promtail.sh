#!/sbin/openrc-run

name=$RC_SVCNAME
description="promtail"
command="/usr/local/bin/promtail"
command_args="-config.file=/etc/promtail/promtail.yaml"
command_user="promtail"
command_background="yes"
pidfile="/run/$RC_SVCNAME.pid"

depend() {
	after net
}
