FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY workflow-gateway/go.mod workflow-gateway/go.mod
COPY workflow-gateway/go.sum workflow-gateway/go.sum
COPY api/ api/
WORKDIR /workspace/workflow-gateway
RUN go mod download

COPY workflow-gateway/cmd/ cmd/
COPY workflow-gateway/internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o gateway ./cmd

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/gateway .
USER 65532:65532

EXPOSE 8080
ENTRYPOINT ["/gateway"]
