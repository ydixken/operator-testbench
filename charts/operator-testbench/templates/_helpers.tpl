{{- define "testbench.fullname" -}}
{{- if contains .Chart.Name .Release.Name }}{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}{{ end }}
{{- end }}

{{- define "testbench.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{/*
The scenario names of the selected pack. For an external pack they come from
the scenarios/ entries of externalPack.files; otherwise from
files/scenarios.yaml, which task chart:generate writes from the embedded
packs. A pack that is missing there means the chart and the image come from
different releases.
*/}}
{{- define "testbench.scenarios" -}}
{{- if .Values.externalPack.configMap }}
{{- $files := .Values.externalPack.files | default list }}
{{- if not (has "operator.yaml" $files) }}{{ fail "externalPack.files must list operator.yaml" }}{{ end }}
{{- $list := list }}
{{- range $files }}
{{- if not (regexMatch "^[a-z0-9._-]+(/[a-z0-9._-]+)?$" .) }}
{{- fail (printf "externalPack.files: %q must be a path of lower-case letters, digits, '.', '_' or '-', at most one directory deep" .) }}
{{- end }}
{{- if and (hasPrefix "scenarios/" .) (hasSuffix ".yaml" .) }}{{ $list = append $list (trimSuffix ".yaml" (trimPrefix "scenarios/" .)) }}{{ end }}
{{- end }}
{{- if not $list }}{{ fail "externalPack.files must list at least one scenarios/<name>.yaml" }}{{ end }}
{{- toYaml $list }}
{{- else }}
{{- $all := .Files.Get "files/scenarios.yaml" | fromYaml }}
{{- $list := get $all .Values.pack }}
{{- if not $list }}{{ fail (printf "pack %q is not in files/scenarios.yaml" .Values.pack) }}{{ end }}
{{- toYaml $list }}
{{- end }}
{{- end }}

{{/*
Every scenario namespace of the run, named the way the engine names them:
testbench-<pack>-<scenario>, with -<N> on Ginkgo process N > 1.
*/}}
{{- define "testbench.namespaces" -}}
{{- $names := list }}
{{- range $proc := untilStep 1 (int (add1 .Values.procs)) 1 }}
{{- range include "testbench.scenarios" $ | fromYamlArray }}
{{- $ns := printf "testbench-%s-%s" $.Values.pack . }}
{{- if gt $proc 1 }}{{ $ns = printf "%s-%d" $ns $proc }}{{ end }}
{{- $names = append $names $ns }}
{{- end }}
{{- end }}
{{- toYaml $names }}
{{- end }}
