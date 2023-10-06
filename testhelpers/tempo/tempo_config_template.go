package tempo

const ConfigTemplate = `
server:
  http_listen_port: {{ .HTTPListenPort }}

  http_tls_config:
    client_auth_type: NoClientCert

  grpc_tls_config:
    client_auth_type: NoClientCert

distributor:
  receivers:
    otlp:
      protocols:
        http:
        grpc:

ingester:
  max_block_duration: 5m               # cut the headblock when this much time passes. this is being set for demo purposes and should probably be left alone normally

compactor:
  compaction:
    block_retention: 1h                # overall Tempo trace retention. set for demo purposes

metrics_generator:
  registry:
    external_labels:
      source: tempo
      cluster: ginkgo-oats
  storage:
    path: /tmp/tempo/generator/wal
    remote_write:
      - url: "{{ .PrometheusEndpoint -}}/api/v1/write"
        send_exemplars: true

storage:
  trace:
    backend: local                     # backend configuration to use
    wal:
      path: /tmp/tempo/wal             # where to store the the wal locally
    local:
      path: /tmp/tempo/blocks

overrides:
  metrics_generator_processors: [service-graphs, span-metrics] # enables metrics generator
`

type ConfigTemplateData struct {
	PrometheusEndpoint string
	HTTPListenPort     int
}
