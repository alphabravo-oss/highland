{{/*
Expand the name of the chart.
*/}}
{{- define "highland.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "highland.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "highland.labels" -}}
helm.sh/chart: {{ include "highland.name" . }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "highland.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "highland.longhornNamespace" -}}
{{- if .Values.embeddedLonghorn.enabled -}}
{{- .Release.Namespace -}}
{{- else -}}
{{- .Values.longhorn.namespace -}}
{{- end -}}
{{- end }}

{{- define "highland.managerUrl" -}}
http://{{ .Values.longhorn.managerService }}.{{ include "highland.longhornNamespace" . }}.svc.cluster.local:{{ .Values.longhorn.managerPort }}
{{- end }}

{{/* providers.longhorn.enabled overrides the legacy longhorn.enabled switch
when explicitly set; null preserves upgrades from Longhorn-only values. */}}
{{- define "highland.longhornEnabled" -}}
{{- if kindIs "bool" .Values.providers.longhorn.enabled -}}
{{- .Values.providers.longhorn.enabled -}}
{{- else -}}
{{- or .Values.longhorn.enabled .Values.embeddedLonghorn.enabled -}}
{{- end -}}
{{- end }}

{{- define "highland.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s" (include "highland.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Validate an image digest. Must be "sha256:" followed by enough hex (64).
*/}}
{{- define "highland.validateDigest" -}}
{{- $digest := . -}}
{{- if $digest -}}
{{- if not (hasPrefix "sha256:" $digest) -}}
{{- fail (printf "image digest must start with sha256:, got %q" $digest) -}}
{{- end -}}
{{- if lt (len $digest) 71 -}}
{{- fail (printf "image digest too short (need sha256: + 64 hex chars), got length %d" (len $digest)) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Render repository:tag or repository@digest.
Dict keys: repository (required), tag (required when digest empty), digest (optional).
*/}}
{{- define "highland.imageRef" -}}
{{- $repo := required "image repository is required" .repository -}}
{{- $digest := default "" .digest -}}
{{- if $digest -}}
{{- include "highland.validateDigest" $digest -}}
{{- printf "%s@%s" $repo $digest -}}
{{- else -}}
{{- $tag := required "image tag is required when digest is empty" .tag -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end }}

{{- define "highland.apiImage" -}}
{{- $tag := .Values.image.api.tag | default .Chart.AppVersion -}}
{{- include "highland.imageRef" (dict "repository" .Values.image.api.repository "tag" $tag "digest" (.Values.image.api.digest | default "")) -}}
{{- end }}

{{- define "highland.webImage" -}}
{{- $tag := .Values.image.web.tag | default .Chart.AppVersion -}}
{{- include "highland.imageRef" (dict "repository" .Values.image.web.repository "tag" $tag "digest" (.Values.image.web.digest | default "")) -}}
{{- end }}

{{/*
fio helper image. Accepts structured {repository,tag,digest} or a legacy string
such as "xridge/fio:latest".
*/}}
{{- define "highland.fioImage" -}}
{{- $img := .Values.benchmark.fioImage -}}
{{- if kindIs "string" $img -}}
{{- $img -}}
{{- else if kindIs "map" $img -}}
{{- $tag := default "3.39" $img.tag -}}
{{- include "highland.imageRef" (dict "repository" $img.repository "tag" $tag "digest" (default "" $img.digest)) -}}
{{- else -}}
{{- fail "benchmark.fioImage must be a string or {repository,tag,digest} map" -}}
{{- end -}}
{{- end }}

{{/*
Soft preferred pod anti-affinity for a component (api|web).
Context: root; dict key "component".
*/}}
{{- define "highland.podAntiAffinity" -}}
{{- $component := .component -}}
{{- $all := .root.Values.podAntiAffinity | default dict -}}
{{- $cfg := index $all $component | default dict -}}
{{- if $cfg.enabled -}}
podAntiAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
    - weight: {{ default 100 $cfg.weight }}
      podAffinityTerm:
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: {{ include "highland.name" .root }}
            app.kubernetes.io/instance: {{ .root.Release.Name }}
            app.kubernetes.io/component: {{ $component }}
        topologyKey: {{ default "kubernetes.io/hostname" $cfg.topologyKey | quote }}
{{- end -}}
{{- end }}

{{/*
Topology spread constraints for a component. Context: root + component.
*/}}
{{- define "highland.topologySpreadConstraints" -}}
{{- $component := .component -}}
{{- $all := .root.Values.topologySpread | default dict -}}
{{- $cfg := index $all $component | default dict -}}
{{- if $cfg.enabled -}}
- maxSkew: {{ default 1 $cfg.maxSkew }}
  topologyKey: {{ default "kubernetes.io/hostname" $cfg.topologyKey | quote }}
  whenUnsatisfiable: {{ default "ScheduleAnyway" $cfg.whenUnsatisfiable }}
  labelSelector:
    matchLabels:
      app.kubernetes.io/name: {{ include "highland.name" .root }}
      app.kubernetes.io/instance: {{ .root.Release.Name }}
      app.kubernetes.io/component: {{ $component }}
{{- end -}}
{{- end }}
