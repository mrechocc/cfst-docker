FROM golang:1.23-bookworm AS builder

ARG CFST_VERSION=v2.3.5

RUN apt-get update \
    && apt-get install --no-install-recommends -y git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --branch "$CFST_VERSION" --depth 1 https://github.com/XIU2/CloudflareSpeedTest.git . \
    && test "$(git describe --exact-match --tags HEAD)" = "$CFST_VERSION" \
    && CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${CFST_VERSION}" \
        -o /out/cfst .

FROM scratch

COPY --from=builder /out/cfst /app/cfst
COPY --from=builder /src/ip.txt /app/ip.txt
COPY --from=builder /src/ipv6.txt /app/ipv6.txt
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

WORKDIR /app
USER 65532:65532
ENTRYPOINT ["/app/cfst"]
