#!/usr/bin/env bash
# A narrated walkthrough of tiny-s3 with the real aws CLI v2, for live demos.
# It builds the image, runs it on a fresh volume in its own network (the
# Compose stack and port 9000 are untouched), and shows each operation,
# pausing before every step. DEMO_PAUSE=0 runs it straight through, as
# make e2e does. Everything it creates is removed at the end.
set -euo pipefail

# shellcheck source=scripts/e2e/lib.sh
. "$(dirname "$0")/e2e/lib.sh"

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

DEMO_PAUSE=${DEMO_PAUSE:-1}
step_no=0

# step announces a step, then waits for Enter unless DEMO_PAUSE=0.
step() {
	step_no=$((step_no + 1))
	printf '\n== Step %d: %s ==\n' "$step_no" "$1"
	shift
	if [ $# -gt 0 ]; then printf '%s\n' "$@"; fi
	if [ "$DEMO_PAUSE" != 0 ]; then
		read -r -p "Press Enter to run it... " _ || true
	fi
}

# aws_cli shows an aws command as the audience would type it, then runs it in
# the client container against the demo server.
aws_cli() {
	printf '$ aws %s\n' "$*"
	in_client aws --endpoint-url http://s3d:9000 "$@"
}

# shell shows and runs a shell command in the client container.
shell() {
	printf '$ %s\n' "$1"
	in_client sh -c "cd /work && $1"
}

step "Build the image and start a fresh server" \
	"tiny-s3 runs in Docker with its data on a named volume; nothing else is needed."
build_images
start_run
docker logs "$SERVER" 2>&1 | sed 's/^/  log: /'

step "Create a bucket and check it exists"
aws_cli s3api create-bucket --bucket demo
aws_cli s3api head-bucket --bucket demo

step "Upload a file with a content type and metadata" \
	"The ETag is the MD5 of the bytes; the server answers only once they are durable."
in_client sh -c 'mkdir -p /work && head -c 1048576 /dev/urandom > /work/photo.jpg'
shell "md5sum photo.jpg"
aws_cli s3api put-object --bucket demo --key photos/2026/photo.jpg --body /work/photo.jpg \
	--content-type image/jpeg --metadata camera=demo

step "Download it and compare byte for byte"
aws_cli s3api get-object --bucket demo --key photos/2026/photo.jpg /work/copy.jpg
shell "cmp photo.jpg copy.jpg && echo identical"

step "Read only the first 100 bytes" \
	"Range requests let clients resume downloads and read parts of large objects."
aws_cli s3api get-object --bucket demo --key photos/2026/photo.jpg --range bytes=0-99 /work/part.bin
shell "wc -c < part.bin"

step "Browse the bucket like folders"
for key in photos/2026/a.jpg photos/2025/b.jpg notes.txt; do
	in_client aws --endpoint-url http://s3d:9000 s3api put-object --bucket demo --key "$key" --body /work/part.bin >/dev/null
done
aws_cli s3api list-objects-v2 --bucket demo --prefix photos/ --delimiter / \
	--query '{folders: CommonPrefixes[].Prefix, files: Contents[].Key}'
aws_cli s3 ls s3://demo/photos/2026/

step "Kill the server in the middle of an upload" \
	"A slow upload is sending when the container gets SIGKILL; after a restart" \
	"only complete objects exist, and the startup log says what was cleaned."
in_client sh -c 'head -c 33554432 /dev/urandom > /work/big.bin; printf "[default]\ns3 =\n  max_bandwidth = 4MB/s\n  multipart_threshold = 1GB\n" > /work/slow.cfg'
printf '$ aws s3 cp big.bin s3://demo/big.bin   # throttled to 4 MB/s\n'
in_client sh -c 'AWS_CONFIG_FILE=/work/slow.cfg AWS_MAX_ATTEMPTS=1 aws --endpoint-url http://s3d:9000 s3 cp --quiet /work/big.bin s3://demo/big.bin' >/dev/null 2>&1 &
upload=$!
until [ "$(count_files tmp)" -ge 1 ]; do sleep 0.1; done
printf '$ docker kill -s KILL tiny-s3   # while the upload is sending\n'
docker kill -s KILL "$SERVER" >/dev/null
wait "$upload" || echo "  (the upload failed, as expected)"
printf '$ docker start tiny-s3\n'
docker start "$SERVER" >/dev/null
wait_listening 2
docker logs "$SERVER" 2>&1 | grep '"msg":"recovery"' | tail -1 | sed 's/^/  log: /'
aws_cli s3api list-objects-v2 --bucket demo --query 'Contents[].Key'

step "Delete an object, twice" "Deletes are idempotent: the second one succeeds too."
aws_cli s3api delete-object --bucket demo --key notes.txt
aws_cli s3api delete-object --bucket demo --key notes.txt
aws_cli s3api head-object --bucket demo --key notes.txt || echo "  (404: the object is gone)"

step "List buckets"
aws_cli s3api list-buckets --query 'Buckets[].[Name,CreationDate]'

step "Clean up" "The demo's containers, network and volume are removed."
cleanup
echo "demo complete"
