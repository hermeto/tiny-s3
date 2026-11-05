# shellcheck shell=bash
# Shared by scripts/e2e/run.sh and scripts/demo.sh; sourced, not executed.
#
# A run owns one network, one data volume and three containers, all named
# tiny-s3-e2e-<pid>-*: the server built from the repository's Dockerfile, a
# client (the e2e image: aws CLI v2 and cmp) that runs the checks through
# docker exec, and an inspector that mounts the volume read-only. Nothing
# publishes a port or uses the Compose project's names, so a running Compose
# stack is not disturbed. Host-side code must stay Bash 3.2 compatible
# (macOS): no associative arrays, mapfile, wait -n or ${v,,}.

E2E_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

# The Go image is defined once, in the Makefile.
GO_IMAGE=$(sed -n 's/^GO_IMAGE := //p' "$E2E_ROOT/Makefile")
SERVER_IMAGE=tiny-s3-e2e-server
CLIENT_IMAGE=tiny-s3-e2e

RUN_ID="tiny-s3-e2e-$$"
NET="$RUN_ID-net"
VOL="$RUN_ID-data"
SERVER="$RUN_ID-s3d"
CLIENT="$RUN_ID-client"
INSPECT="$RUN_ID-inspect"

# build_images builds the server image from the repository's Dockerfile and
# the client image from e2e/Dockerfile.
build_images() {
	if [ -z "$GO_IMAGE" ]; then
		echo "GO_IMAGE not found in $E2E_ROOT/Makefile" >&2
		return 1
	fi
	docker build -q -t "$SERVER_IMAGE" "$E2E_ROOT" >/dev/null
	docker build -q --build-arg GO_IMAGE="$GO_IMAGE" -t "$CLIENT_IMAGE" \
		-f "$E2E_ROOT/e2e/Dockerfile" "$E2E_ROOT/e2e" >/dev/null
}

# cleanup removes everything the run created; safe to call more than once.
cleanup() {
	docker rm -f "$SERVER" "$CLIENT" "$INSPECT" >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
	docker volume rm "$VOL" >/dev/null 2>&1 || true
}

# start_run creates the network and a fresh volume, then starts the server,
# the client (with the checks copied into /suite) and the inspector.
start_run() {
	docker network create "$NET" >/dev/null
	docker volume create "$VOL" >/dev/null
	docker run -d --name "$SERVER" --network "$NET" --network-alias s3d \
		-v "$VOL:/data" "$SERVER_IMAGE" >/dev/null
	docker run -d --name "$CLIENT" --network "$NET" \
		-e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test \
		-e AWS_DEFAULT_REGION=us-east-1 -e AWS_PAGER= \
		-e S3_ENDPOINT=http://s3d:9000 \
		"$CLIENT_IMAGE" sleep infinity >/dev/null
	docker cp "$E2E_ROOT/scripts/e2e/." "$CLIENT:/suite" >/dev/null
	docker run -d --name "$INSPECT" --network none -v "$VOL:/data:ro" \
		"$CLIENT_IMAGE" sleep infinity >/dev/null
	wait_listening 1
}

# wait_listening waits until the server has logged its n-th listening line
# (n counts starts of the same container).
wait_listening() {
	local n=$1 tries=0
	while [ "$tries" -lt 300 ]; do
		tries=$((tries + 1))
		if [ "$(docker logs "$SERVER" 2>/dev/null | grep -c '"msg":"listening"')" -ge "$n" ]; then
			return 0
		fi
		sleep 0.1
	done
	echo "server did not log listening #$n within 30s" >&2
	docker logs "$SERVER" 2>&1 | tail -20 >&2
	return 1
}

# in_client runs a command in the client container.
in_client() {
	docker exec "$CLIENT" "$@"
}

# in_inspector runs a command in the inspector container.
in_inspector() {
	docker exec "$INSPECT" "$@"
}

# count_files prints the number of entries in a directory of the volume.
count_files() {
	in_inspector sh -c "ls -A '/data/$1' | wc -l" | tr -d ' '
}
