# Every target runs inside the pinned Go container, so the host needs only
# Docker and make. Keep this file compatible with GNU make 3.81 (macOS default):
# no .ONESHELL, no $(file ...), no grouped targets.

GO_IMAGE := golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385

# Allocate a TTY only when make runs interactively. $(shell) captures stdout,
# so test stdin instead.
TTY := $(shell test -t 0 && echo -t)

# Module and build caches live in named volumes so repeat runs download nothing
# and no root-owned cache files land in the checkout.
DOCKER_RUN := docker run --rm $(TTY) \
	-v "$(CURDIR)":/src -w /src \
	-v tiny-s3-gomod:/go/pkg/mod \
	-v tiny-s3-gobuild:/root/.cache/go-build \
	-e GOTOOLCHAIN=local \
	-e GOFLAGS=-mod=readonly \
	$(GO_IMAGE)

.PHONY: test race vet cover e2e clean-cache

test:
	$(DOCKER_RUN) go test ./...

race:
	$(DOCKER_RUN) go test -race ./...

# The e2e tag makes vet check the e2e package too.
vet:
	$(DOCKER_RUN) go vet -tags e2e ./...

# Fails unless every package under internal/ has tests and 100% statement
# coverage. cmd/ is covered by e2e tests instead.
cover:
	$(DOCKER_RUN) bash scripts/cover-gate.sh

# Builds the real s3d binary and drives it with real signals (e2e/).
e2e:
	$(DOCKER_RUN) go test -tags e2e -count=1 ./e2e/...

clean-cache:
	docker volume rm -f tiny-s3-gomod tiny-s3-gobuild
