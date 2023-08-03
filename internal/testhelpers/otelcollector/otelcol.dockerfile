FROM golang:1.19.12-bullseye AS builder

WORKDIR /usr/bin/otelcol

COPY otelcol-builder-manifest.yaml ./
RUN go install go.opentelemetry.io/collector/cmd/builder@v0.66.0
RUN builder --config=otelcol-builder-manifest.yaml

FROM gcr.io/distroless/base:latest
COPY --from=builder /usr/bin/otelcol/_build/otelcol /otelcol
ENTRYPOINT ["/otelcol"]
