# Every target runs inside the pinned Go container, so the host needs only
# Docker and make. Keep this file compatible with GNU make 3.81 (macOS default):
# no .ONESHELL, no $(file ...), no grouped targets.

GO_IMAGE := golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385

# Allocate a TTY only when make runs interactively. $(shell) captures stdout,
# so test stdin instead.
TTY := $(shell test -t 0 && echo -t)

# Module and build caches live in named volumes so repeat runs download nothing
# and no root-owned cache files land in the checkout. Each value is the source
# of a `docker run -v` mount: a bare name is a named volume, an absolute path
# is a bind mount (CI points both at runner directories it saves between runs).
# The names deliberately differ from Go's GOMODCACHE/GOCACHE: make imports the
# environment, so a host Go setup must never leak into the container.
GOMOD_CACHE ?= tiny-s3-gomod
GOBUILD_CACHE ?= tiny-s3-gobuild

# DOCKER_RUN_IN is the docker run prefix without the image, shared by the Go
# image and the e2e image.
DOCKER_RUN_IN := docker run --rm $(TTY) \
	-v "$(CURDIR)":/src -w /src \
	-v "$(GOMOD_CACHE)":/go/pkg/mod \
	-v "$(GOBUILD_CACHE)":/root/.cache/go-build \
	-e GOTOOLCHAIN=local \
	-e GOFLAGS=-mod=readonly

DOCKER_RUN := $(DOCKER_RUN_IN) $(GO_IMAGE)

# The e2e image adds the AWS CLI v2 to the same Go toolchain (e2e/Dockerfile).
E2E_IMAGE := tiny-s3-e2e

.PHONY: test race vet cover e2e shellcheck clean-cache

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

# Three steps, each failing the target: the Go tests that build the real s3d
# binary and drive it with real signals and the aws CLI (e2e/); the container
# suite against the image built from the Dockerfile (scripts/e2e/run.sh); and
# the demo without pauses, so it keeps working. Needs Docker and Bash.
e2e:
	docker build -q --build-arg GO_IMAGE=$(GO_IMAGE) -t $(E2E_IMAGE) -f e2e/Dockerfile e2e
	$(DOCKER_RUN_IN) $(E2E_IMAGE) go test -tags e2e -count=1 ./e2e/...
	bash scripts/e2e/run.sh
	DEMO_PAUSE=0 bash scripts/demo.sh

# Lints every shell script; the image is pinned like the Go one.
SHELLCHECK_IMAGE := koalaman/shellcheck:v0.10.0@sha256:2097951f02e735b613f4a34de20c40f937a6c8f18ecb170612c88c34517221fb

shellcheck:
	docker run --rm -v "$(CURDIR)":/src -w /src $(SHELLCHECK_IMAGE) scripts/*.sh scripts/e2e/*.sh

clean-cache:
	docker volume rm -f tiny-s3-gomod tiny-s3-gobuild
