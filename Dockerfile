# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
ARG TARGETARCH="amd64"
ARG BUILDPLATFORM="linux"
FROM --platform=$BUILDPLATFORM golang:1.25 AS build

WORKDIR /go/src/github.com/prometheus-community/ipmi_exporter
ADD . .

# Build the ipmi_exporter command inside the container.
# Use git commands if available, otherwise fallback to environment variables
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -a -tags 'netgo static_build' -ldflags "-X github.com/prometheus/common/version.Version=$(git describe --tags --abbrev=0 2>/dev/null || echo 'dev') -X github.com/prometheus/common/version.Revision=$(git rev-parse HEAD 2>/dev/null || echo 'unknown') -X github.com/prometheus/common/version.Branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo 'unknown') -X github.com/prometheus/common/version.BuildUser=goreleaser -X github.com/prometheus/common/version.BuildDate=$(date --iso-8601=seconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S')" -o ipmi_exporter .

# Use Alpine Linux for the final image
FROM alpine:3

# Install freeipmi and ca-certificates for HTTPS
RUN apk --no-cache add freeipmi ca-certificates tzdata

LABEL maintainer="The Prometheus Authors <prometheus-developers@googlegroups.com>" \
      org.opencontainers.image.title="IPMI Exporter" \
      org.opencontainers.image.description="Prometheus exporter for IPMI metrics" \
      org.opencontainers.image.licenses="Apache-2.0"

# Copy the binary from build stage
COPY --from=build /go/src/github.com/prometheus-community/ipmi_exporter/ipmi_exporter /bin/ipmi_exporter

# Set permissions
RUN chmod +x /bin/ipmi_exporter

# Expose the metrics port
EXPOSE 9290

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:9290/metrics || exit 1

ENTRYPOINT ["/bin/ipmi_exporter"]
