#!/sbin/openrc-run

name=$RC_SVCNAME
description="promtail"
supervisor="supervise-daemon"
command="/usr/local/bin/promtail"
command_args="-config.file=/etc/promtail/promtail.yaml"
command_user="promtail"


depend() {
	after net
}
