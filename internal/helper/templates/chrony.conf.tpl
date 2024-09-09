{{- range .pools -}}
pool {{ . }} iburst
{{ end -}}
{{- range .servers -}}
server {{ . }} iburst
{{ end -}}

# Settings from default chrony config
initstepslew 10 {{ concat .pools .servers | join " " }}
driftfile /var/lib/chrony/chrony.drift
rtcsync
cmdport 0
