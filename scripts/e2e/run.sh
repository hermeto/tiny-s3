#!/usr/bin/env bash
# The container suite: builds the server image from the repository's
# Dockerfile, runs it on a fresh named volume, and drives every operation with
# the real aws CLI v2 (default settings) from a client container, including a
# SIGKILL during parallel uploads. Exits 0 when every check passes, 1 at the
# first failure. Contract: specs/014-e2e-suite-demo/contracts/suite.md.
set -euo pipefail

# shellcheck source=scripts/e2e/lib.sh
. "$(dirname "$0")/lib.sh"

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# suite_failed prints the verdict and the server's last log lines.
suite_failed() {
	echo "e2e suite: FAILED" >&2
	echo "--- last server log lines ($SERVER) ---" >&2
	docker logs "$SERVER" 2>&1 | tail -20 >&2 || true
	exit 1
}

echo "building images"
build_images
echo "starting run $RUN_ID"
start_run

for group in bucket object range list delete; do
	in_client bash /suite/checks.sh "$group" || suite_failed
done

# check_fail fails the suite from the host side, in the checks' format.
check_fail() {
	echo "FAIL $1: $2"
	suite_failed
}

# acks prints how many crash uploads the client saw acknowledged so far.
acks() {
	in_client sh -c 'ls /work/acks 2>/dev/null | wc -l' | tr -d ' '
}

# Crash test: kill the server once the ten fast uploads are acknowledged and
# at least five throttled ones are in flight, restart it on the same volume,
# verify what it serves.
in_client bash /suite/checks.sh crash-upload &
uploads=$!
tries=0
until [ "$(acks)" -ge 10 ] && [ "$(count_files tmp)" -ge 5 ]; do
	tries=$((tries + 1))
	if [ "$tries" -gt 1200 ] || [ "$(acks)" -ge 20 ]; then
		wait "$uploads" || true
		check_fail crash/kill "never saw 10 acknowledged uploads with 5 more in flight (acks: $(acks))"
	fi
	sleep 0.05
done
docker kill -s KILL "$SERVER" >/dev/null
echo "PASS crash/kill (SIGKILL after $(acks) acknowledged uploads, with $(count_files tmp) temp files in tmp/)"
wait "$uploads" || check_fail crash/kill "the upload script failed"

docker start "$SERVER" >/dev/null
wait_listening 2 || check_fail crash/recovery "the server did not restart"
recovery=$(docker logs "$SERVER" 2>/dev/null | grep '"msg":"recovery"' | sed -n 2p)
[ -n "$recovery" ] || check_fail crash/recovery "no recovery line after the restart"
removed=$(printf '%s' "$recovery" | sed -n 's/.*"tmp_removed":\([0-9]*\).*/\1/p')
[ "${removed:-0}" -ge 1 ] || check_fail crash/recovery "the restart removed no temp file: the kill did not land mid-upload"
[ "$(count_files tmp)" -eq 0 ] || check_fail crash/recovery "tmp/ is not empty after the restart"
echo "PASS crash/recovery ${recovery#*\"msg\":\"recovery\",}"

objects=$(in_client bash /suite/checks.sh crash-verify) || {
	printf '%s\n' "$objects"
	suite_failed
}
printf '%s\n' "$objects" | grep '^PASS'
total=$(printf '%s\n' "$objects" | tail -1)
[ "$(count_files data)" -eq "$total" ] ||
	check_fail crash/etags "data/ holds $(count_files data) bodies for $total listed objects"
echo "PASS crash/bodies ($total objects, $total bodies)"

echo "e2e suite: PASSED"
