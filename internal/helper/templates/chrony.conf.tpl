{{- range .pools -}}
pool {{ . }} iburst
{{ end -}}
{{- range .servers -}}
server {{ . }} iburst
{{ end -}}

# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
