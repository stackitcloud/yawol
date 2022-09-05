#!/sbin/openrc-run

name=$RC_SVCNAME
description="keepalivedstats"
supervisor="supervise-daemon"
command="/usr/local/bin/keepalivedstats-script.sh"
command_user="root"

depend() {
	after net
}
