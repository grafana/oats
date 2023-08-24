package mimir

const ConfigTemplate = `
multitenancy_enabled: true

server:
  http_listen_port: 9009
  grpc_listen_port: 9095

distributor:
  ring:
    kvstore:
      store: "memberlist"

ingester:
  ring:
    final_sleep: "0s"
    num_tokens: 512
    replication_factor: 1

ruler:
  poll_interval: "2s"
  rule_path: "/tmp/mimir/ruler"

alertmanager:
  data_dir: "/tmp/mimir/alertmanager"
  sharding_ring:
    replication_factor: 1

ingester_client:
  grpc_client_config:
    max_recv_msg_size: 104_857_600
    max_send_msg_size: 104_857_600
    grpc_compression: "gzip"

blocks_storage:
  tsdb:
    dir: "/tmp/mimir/tsdb"
    ship_interval: "1m"
    block_ranges_period:
      - "2h"
    retention_period: "3h"
  bucket_store:
    sync_dir: "/tmp/mimir/tsdb_bucket_store"

compactor:
  data_dir: "/tmp/mimir/compactor"

store_gateway:
  sharding_ring:
    replication_factor: 1
`
