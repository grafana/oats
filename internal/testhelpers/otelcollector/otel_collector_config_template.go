package otelcollector

const ConfigTemplate = `
receivers:
  otlp:
    protocols:
      grpc:

exporters:
  otlp:
    endpoint: {{ .TempoEndpoint }}
    tls:
      insecure: true
  file:
    path: {{ .FileExporterOutputPath }} 

extensions:
  health_check:
     endpoint: 0.0.0.0:{{- .HealthCheckPort }}
     path: "/health/status" 

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp, file]

  extensions: [health_check]

`

type ConfigTemplateData struct {
	TempoEndpoint          string
	FileExporterOutputPath string
	HealthCheckPort        string
}
