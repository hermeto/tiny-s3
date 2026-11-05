# e2e test image: the official AWS CLI v2 build plus the same pinned Go
# toolchain every other make target uses (passed in by the Makefile), so the
# e2e tests can drive s3d with the real `aws s3api` binary. It is also the
# client container of the container suite (scripts/e2e), which needs cmp.
ARG GO_IMAGE
FROM ${GO_IMAGE} AS go

FROM amazon/aws-cli:2.31.11@sha256:7df59f7bf15fd670eef5a08dfd4b1da6f4c1de8edd9e74b19574d1db1794a884
# Amazon Linux 2023 locks its repositories to the image's release; the exact
# version makes the build fail rather than drift if that ever changes.
RUN dnf install -y --setopt=install_weak_deps=False diffutils-3.8-1.amzn2023.0.2 \
	&& dnf clean all
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
	GOPATH=/go \
	CGO_ENABLED=0
# The base image's entrypoint is `aws`; tests run `go test` directly.
ENTRYPOINT []
