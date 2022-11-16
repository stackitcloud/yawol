#!/sbin/openrc-run

[ -f /etc/yawol/env.conf ] && . /etc/yawol/env.conf

name=$RC_SVCNAME
description="yawollet"
command="/usr/local/bin/yawollet"
command_args="$YAWOLLET_ARGS"
command_background="yes"
command_user="yawol"
output_logger="logger -t yawollet"
error_logger="logger -t yawollet"
pidfile="/run/$RC_SVCNAME.pid"

depend() {
        after net
}
