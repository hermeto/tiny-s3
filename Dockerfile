# Build stage: the same pinned toolchain the Makefile uses.
FROM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags="-s -w" -o /out/s3d ./cmd/s3d \
	&& mkdir /out/data

# Runtime: static binary on distroless, no shell, non-root (UID/GID 65532).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=build /out/s3d /s3d
# Docker seeds an empty named volume from this directory, ownership included,
# so the non-root user can write /data without a chown at runtime.
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
EXPOSE 9000
# Exec form: s3d is PID 1 and receives SIGTERM directly.
ENTRYPOINT ["/s3d"]
CMD ["-listen=:9000", "-data-dir=/data"]
