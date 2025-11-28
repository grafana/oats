FROM ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-java:2.22.0@sha256:bab9caa7a68f4490722b7c299aa7875f6444b73b05384bb4f1919bb4a466df24 AS agent

FROM springio/petclinic:latest@sha256:fbb6f32dff6b4b05aa0bb4252a674a2084ecd26137793c88213385e62a6ea171

WORKDIR /app/

COPY --from=agent --chown=cnb /javaagent.jar /app/javaagent.jar

ENV JAVA_TOOL_OPTIONS=-javaagent:/app/javaagent.jar
