#!/bin/sh

while true
do
  kill -USR2 $(cat /var/run/keepalived.pid)
  chmod 644 /tmp/keepalived.stats
  sleep 20s
done
