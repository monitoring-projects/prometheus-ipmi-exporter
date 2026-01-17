# Use Alpine Linux for the final image
FROM alpine:3

# Install freeipmi and ca-certificates for HTTPS
RUN apk --no-cache add freeipmi ca-certificates tzdata

LABEL maintainer="The Prometheus Authors <prometheus-developers@googlegroups.com>" \
      org.opencontainers.image.title="IPMI Exporter" \
      org.opencontainers.image.description="Prometheus exporter for IPMI metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.source="https://github.com/monitoring-projects/prometheus-ipmi-exporter"

# Copy the pre-built binary from GoReleaser
COPY ipmi_exporter /bin/ipmi_exporter

# Set permissions
RUN chmod +x /bin/ipmi_exporter

# Expose the metrics port
EXPOSE 9290

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:9290/metrics || exit 1

ENTRYPOINT ["/bin/ipmi_exporter"]
