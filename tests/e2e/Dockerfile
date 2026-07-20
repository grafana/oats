FROM ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-java:2.22.0@sha256:df7cae821770cadb6dbe7e36681008c8e9dff71bc5f5c8deb0d8422b541592c5 AS agent

FROM springio/petclinic:latest@sha256:fbb6f32dff6b4b05aa0bb4252a674a2084ecd26137793c88213385e62a6ea171

WORKDIR /app/

COPY --from=agent --chown=cnb /javaagent.jar /app/javaagent.jar

ENV JAVA_TOOL_OPTIONS=-javaagent:/app/javaagent.jar
