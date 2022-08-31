#!/sbin/openrc-run

name=$RC_SVCNAME
description="keepalivedstats"
supervisor="supervise-daemon"
command="/usr/local/bin/keepalivedstats-script.sh"
command_user="root"
retry=10800
respawn_delay=2
respawn_max=50

depend() {
	after net
}
