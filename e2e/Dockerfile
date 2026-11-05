# e2e test image: the official AWS CLI v2 build plus the same pinned Go
# toolchain every other make target uses (passed in by the Makefile), so the
# e2e tests can drive s3d with the real `aws s3api` binary.
ARG GO_IMAGE
FROM ${GO_IMAGE} AS go

FROM amazon/aws-cli:2.31.11@sha256:7df59f7bf15fd670eef5a08dfd4b1da6f4c1de8edd9e74b19574d1db1794a884
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
	GOPATH=/go \
	CGO_ENABLED=0
# The base image's entrypoint is `aws`; tests run `go test` directly.
ENTRYPOINT []
