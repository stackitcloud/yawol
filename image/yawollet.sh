#!/sbin/openrc-run

[ -f /etc/yawol/env.conf ] && . /etc/yawol/env.conf

name=$RC_SVCNAME
description="yawollet"
supervisor="supervise-daemon"
command="/usr/local/bin/yawollet"
command_args="$YAWOLLET_ARGS"
command_user="yawol"
output_log="/var/log/yawol/yawollet.log"
error_log="/var/log/yawol/yawollet.log"

depend() {
	after net
}
