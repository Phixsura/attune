{{/*
Common template helpers.
*/}}
{{- define "attune.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "attune.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "attune.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "attune.selectorLabels" -}}
app.kubernetes.io/name: {{ include "attune.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "attune.appSelectorLabels" -}}
app.kubernetes.io/name: {{ include "attune.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: app
{{- end -}}

{{- define "attune.colorLabelKey" -}}
attune.phixsura.dev/color
{{- end -}}

{{- define "attune.rolloutColorLabel" -}}
{{- with .Values.rollout.color }}
{{ include "attune.colorLabelKey" $ }}: {{ . | quote }}
{{- end }}
{{- end -}}

{{- define "attune.trafficServiceName" -}}
{{- default (printf "%s-traffic" (include "attune.fullname" .)) .Values.rollout.trafficService.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "attune.trafficSelectorLabels" -}}
app.kubernetes.io/name: {{ include "attune.name" . }}
app.kubernetes.io/component: app
{{ include "attune.colorLabelKey" . }}: {{ .Values.rollout.trafficService.activeColor | quote }}
{{- end -}}

{{- define "attune.primaryServiceName" -}}
{{- if .Values.rollout.trafficService.enabled -}}
{{- include "attune.trafficServiceName" . -}}
{{- else -}}
{{- include "attune.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "attune.primaryServiceSelectorLabels" -}}
{{- if .Values.rollout.trafficService.enabled -}}
{{ include "attune.selectorLabels" . }}
app.kubernetes.io/component: traffic
{{- else -}}
{{ include "attune.appSelectorLabels" . }}
{{- end -}}
{{- end -}}

{{- define "attune.labels" -}}
helm.sh/chart: {{ include "attune.chart" . }}
{{ include "attune.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "attune.annotations" -}}
{{- with .Values.commonAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "attune.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "attune.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "attune.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{- define "attune.configSecretName" -}}
{{- default (printf "%s-config" (include "attune.fullname" .)) .Values.config.existingSecret -}}
{{- end -}}

{{- define "attune.configPath" -}}
{{- printf "%s/%s" (.Values.config.mountPath | trimSuffix "/") .Values.config.secretKey -}}
{{- end -}}

{{- define "attune.postgresFullname" -}}
{{- printf "%s-postgres" (include "attune.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "attune.postgresSelectorLabels" -}}
app.kubernetes.io/name: {{ include "attune.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: postgres
{{- end -}}

{{- define "attune.postgresSecretName" -}}
{{- default (printf "%s-postgres" (include "attune.fullname" .)) .Values.postgres.embedded.auth.existingSecret -}}
{{- end -}}

{{- define "attune.databaseURL" -}}
{{- if .Values.config.render.databaseURL -}}
{{- .Values.config.render.databaseURL -}}
{{- else if .Values.postgres.embedded.enabled -}}
{{- $user := .Values.postgres.embedded.auth.username | toString -}}
{{- $password := default "" .Values.postgres.embedded.auth.password | toString -}}
{{- $database := .Values.postgres.embedded.auth.database | toString -}}
postgres://{{ $user | urlquery }}:{{ $password | urlquery }}@{{ include "attune.postgresFullname" . }}:{{ .Values.postgres.embedded.service.port }}/{{ $database | urlquery }}?sslmode=disable
{{- else -}}
{{- "" -}}
{{- end -}}
{{- end -}}

{{- define "attune.validate" -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- $appPort := int .Values.app.port -}}
{{- $renderPort := int .Values.config.render.port -}}
{{- $workloadEnabled := .Values.workload.enabled -}}
{{- $trafficServiceName := include "attune.trafficServiceName" . -}}
{{- if and .Values.rollout.trafficService.enabled (not (trim .Values.rollout.trafficService.activeColor)) -}}
{{- fail "rollout.trafficService.activeColor is required when rollout.trafficService.enabled=true" -}}
{{- end -}}
{{- if and .Values.rollout.trafficService.enabled .Values.service.enabled (eq $trafficServiceName (include "attune.fullname" .)) -}}
{{- fail "rollout.trafficService.name must not equal the release Service name when service.enabled=true" -}}
{{- end -}}
{{- if and (not $workloadEnabled) (not .Values.rollout.trafficService.enabled) -}}
{{- fail "workload.enabled=false is only supported for a traffic-service-only release" -}}
{{- end -}}
{{- if and (not $workloadEnabled) .Values.postgres.embedded.enabled -}}
{{- fail "workload.enabled=false requires postgres.embedded.enabled=false" -}}
{{- end -}}
{{- if and (not $workloadEnabled) .Values.helmTest.enabled -}}
{{- fail "workload.enabled=false requires helmTest.enabled=false" -}}
{{- end -}}
{{- if and .Values.postgres.embedded.enabled .Values.postgres.external.enabled -}}
{{- fail "postgres.embedded.enabled and postgres.external.enabled cannot both be true" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.autoscaling.enabled) (eq .Values.profile "production") (lt $replicaCount 2) -}}
{{- fail "profile=production requires replicaCount >= 2 when autoscaling.enabled=false" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) (not .Values.config.render.enabled) -}}
{{- fail "config.existingSecret is required when config.render.enabled=false" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) .Values.config.render.enabled (not (trim .Values.config.render.secrets.tinkKeyset)) -}}
{{- fail "config.render.secrets.tinkKeyset is required when the chart renders config.yaml; use --set-file for dev/CI or config.existingSecret for production" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) .Values.config.render.enabled (ne $appPort $renderPort) -}}
{{- fail "app.port must match config.render.port when the chart renders config.yaml" -}}
{{- end -}}
{{- if and $workloadEnabled .Values.postgres.embedded.enabled (not .Values.postgres.embedded.auth.existingSecret) (not (trim (default "" .Values.postgres.embedded.auth.password))) -}}
{{- fail "postgres.embedded.auth.password is required when embedded Postgres creates its own Secret" -}}
{{- end -}}
{{- if and $workloadEnabled (eq .Values.profile "production") (not .Values.config.existingSecret) -}}
{{- fail "profile=production requires config.existingSecret so production secrets do not enter Helm release history" -}}
{{- end -}}
{{- if and $workloadEnabled (eq .Values.profile "production") .Values.postgres.embedded.enabled -}}
{{- fail "profile=production requires postgres.embedded.enabled=false; use an external PostgreSQL service" -}}
{{- end -}}
{{- if and $workloadEnabled (eq .Values.profile "production") (not .Values.postgres.external.enabled) -}}
{{- fail "profile=production requires postgres.external.enabled=true" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) .Values.config.render.enabled (not .Values.postgres.embedded.enabled) (not (trim .Values.config.render.databaseURL)) -}}
{{- fail "config.render.databaseURL is required when embedded Postgres is disabled and the chart renders config.yaml" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) .Values.config.render.enabled .Values.postgres.external.enabled (not (trim .Values.config.render.databaseURL)) -}}
{{- fail "external Postgres requires config.existingSecret or config.render.databaseURL" -}}
{{- end -}}
{{- if and $workloadEnabled (not .Values.config.existingSecret) .Values.config.render.enabled .Values.postgres.embedded.enabled .Values.postgres.embedded.auth.existingSecret (not (trim .Values.config.render.databaseURL)) -}}
{{- fail "embedded Postgres with auth.existingSecret requires config.existingSecret or config.render.databaseURL" -}}
{{- end -}}
{{- if and .Values.ingress.enabled (empty .Values.ingress.hosts) -}}
{{- fail "ingress.hosts must contain at least one host when ingress.enabled=true" -}}
{{- end -}}
{{- if and .Values.ingress.enabled (not .Values.service.enabled) (not .Values.rollout.trafficService.enabled) -}}
{{- fail "ingress.enabled=true requires service.enabled=true or rollout.trafficService.enabled=true" -}}
{{- end -}}
{{- if and .Values.serviceMonitor.enabled (not .Values.service.enabled) (not .Values.rollout.trafficService.enabled) -}}
{{- fail "serviceMonitor.enabled=true requires service.enabled=true or rollout.trafficService.enabled=true" -}}
{{- end -}}
{{- if and .Values.helmTest.enabled (not .Values.service.enabled) -}}
{{- fail "helmTest.enabled=true requires service.enabled=true" -}}
{{- end -}}
{{- if and $workloadEnabled .Values.podDisruptionBudget.enabled -}}
{{- $minAvailable := int .Values.podDisruptionBudget.minAvailable -}}
{{- if and (not .Values.autoscaling.enabled) (ge $minAvailable $replicaCount) -}}
{{- fail "podDisruptionBudget.minAvailable must be lower than replicaCount when autoscaling.enabled=false" -}}
{{- end -}}
{{- if and .Values.autoscaling.enabled (ge $minAvailable (int .Values.autoscaling.minReplicas)) -}}
{{- fail "podDisruptionBudget.minAvailable must be lower than autoscaling.minReplicas when autoscaling.enabled=true" -}}
{{- end -}}
{{- end -}}
{{- if and $workloadEnabled .Values.autoscaling.enabled (not .Values.autoscaling.targetCPUUtilizationPercentage) (not .Values.autoscaling.targetMemoryUtilizationPercentage) -}}
{{- fail "autoscaling requires at least one target CPU or memory utilization metric" -}}
{{- end -}}
{{- if and $workloadEnabled .Values.autoscaling.enabled -}}
{{- $minReplicas := int .Values.autoscaling.minReplicas -}}
{{- $maxReplicas := int .Values.autoscaling.maxReplicas -}}
{{- if gt $minReplicas $maxReplicas -}}
{{- fail "autoscaling.minReplicas must be less than or equal to autoscaling.maxReplicas" -}}
{{- end -}}
{{- if and (eq .Values.profile "production") (lt $minReplicas 2) -}}
{{- fail "profile=production requires autoscaling.minReplicas >= 2 when autoscaling.enabled=true" -}}
{{- end -}}
{{- if and .Values.autoscaling.targetCPUUtilizationPercentage (not (dig "requests" "cpu" "" .Values.resources)) -}}
{{- fail "autoscaling CPU utilization requires resources.requests.cpu" -}}
{{- end -}}
{{- if and .Values.autoscaling.targetMemoryUtilizationPercentage (not (dig "requests" "memory" "" .Values.resources)) -}}
{{- fail "autoscaling memory utilization requires resources.requests.memory" -}}
{{- end -}}
{{- end -}}
{{- if .Values.networkPolicy.enabled -}}
{{- if empty .Values.networkPolicy.ingress -}}
{{- fail "networkPolicy.ingress must contain at least one rule when networkPolicy.enabled=true" -}}
{{- end -}}
{{- if empty .Values.networkPolicy.egress -}}
{{- fail "networkPolicy.egress must contain at least one rule when networkPolicy.enabled=true" -}}
{{- end -}}
{{- end -}}
{{- end -}}
