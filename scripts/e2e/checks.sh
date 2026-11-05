#!/usr/bin/env bash
# The checks of the container suite. Runs inside the client container (aws
# CLI v2 with its default settings, cmp, jq), one group per call:
#
#   checks.sh bucket|object|range|list|delete|crash-upload|crash-verify
#
# Each check prints "PASS <name>"; the first failure prints
# "FAIL <name>: <reason>" and exits 1. Contract:
# specs/014-e2e-suite-demo/contracts/suite.md.
set -euo pipefail

W=/work
mkdir -p "$W"
cd "$W"

s3() { aws --endpoint-url "$S3_ENDPOINT" --output json s3api "$@"; }
pass() { printf 'PASS %s\n' "$1"; }
fail() {
	printf 'FAIL %s: %s\n' "$1" "$2"
	exit 1
}

# expect_failure NAME CODE ARGS... runs s3api ARGS and requires it to fail
# with CODE (such as "(404)" or "(InvalidRange)") on stderr.
expect_failure() {
	local name=$1 code=$2 err
	shift 2
	if err=$(s3 "$@" 2>&1 >/dev/null); then
		fail "$name" "s3api $* succeeded, want $code"
	fi
	case $err in
	*"$code"*) ;;
	*) fail "$name" "s3api $* failed with: $err; want $code" ;;
	esac
}

quoted_md5() { printf '"%s"' "$(md5sum "$1" | cut -d' ' -f1)"; }

# put NAME KEY FILE uploads FILE and checks the ETag is its quoted MD5.
put() {
	local etag
	etag=$(s3 put-object --bucket demo --key "$2" --body "$3" --query ETag --output text) ||
		fail "$1" "put-object $2 failed"
	[ "$etag" = "$(quoted_md5 "$3")" ] || fail "$1" "ETag $etag, want $(quoted_md5 "$3")"
}

group_bucket() {
	s3 create-bucket --bucket demo >/dev/null || fail bucket/create "create-bucket failed"
	pass bucket/create
	s3 head-bucket --bucket demo >/dev/null || fail bucket/head "head-bucket demo failed"
	expect_failure bucket/head "(404)" head-bucket --bucket missing
	pass bucket/head
}

group_object() {
	printf 'hello world' >small.txt
	head -c 104857600 /dev/urandom >big.bin
	printf 'ünïcødé ✓\n' >unicode.txt
	put object/put-small small.txt small.txt
	pass object/put-small
	put object/put-100MiB big.bin big.bin
	pass object/put-100MiB
	put object/put-unicode "dir/sub/ünïcødé ✓.txt" unicode.txt
	pass object/put-unicode

	local key file got
	for pair in "small.txt|small.txt" "big.bin|big.bin" "dir/sub/ünïcødé ✓.txt|unicode.txt"; do
		key=${pair%%|*}
		file=${pair#*|}
		got=$(s3 head-object --bucket demo --key "$key" --query '[ContentLength,ETag]' --output text) ||
			fail object/head "head-object $key failed"
		[ "$got" = "$(wc -c <"$file" | tr -d ' ')	$(quoted_md5 "$file")" ] ||
			fail object/head "$key: $got"
	done
	pass object/head
	for pair in "small.txt|small.txt" "big.bin|big.bin" "dir/sub/ünïcødé ✓.txt|unicode.txt"; do
		key=${pair%%|*}
		file=${pair#*|}
		s3 get-object --bucket demo --key "$key" out.bin >/dev/null || fail object/get "get-object $key failed"
		cmp -s "$file" out.bin || fail object/get "$key differs from the uploaded file"
	done
	pass object/get
}

group_range() {
	head -c 10240 /dev/urandom >range.bin
	put range/put range.bin range.bin
	s3 get-object --bucket demo --key range.bin --range bytes=0-99 out.bin >/dev/null || fail range/first "get-object failed"
	head -c 100 range.bin | cmp -s - out.bin || fail range/first "bytes=0-99 differs from the first 100 bytes"
	pass range/first
	s3 get-object --bucket demo --key range.bin --range bytes=100- out.bin >/dev/null || fail range/from "get-object failed"
	tail -c +101 range.bin | cmp -s - out.bin || fail range/from "bytes=100- differs from the rest of the file"
	pass range/from
	s3 get-object --bucket demo --key range.bin --range bytes=-50 out.bin >/dev/null || fail range/suffix "get-object failed"
	tail -c 50 range.bin | cmp -s - out.bin || fail range/suffix "bytes=-50 differs from the last 50 bytes"
	pass range/suffix
	expect_failure range/invalid "(InvalidRange)" get-object --bucket demo --key range.bin --range bytes=10240- out.bin
	pass range/invalid
}

group_list() {
	local key got
	printf 'x' >one.txt
	for key in tree/top.txt tree/dir/one.txt tree/dir/two.txt tree/dir/sub/three.txt; do
		s3 put-object --bucket demo --key "$key" --body one.txt >/dev/null || fail list/prefix "put-object $key failed"
	done
	got=$(s3 list-objects-v2 --bucket demo --prefix tree/dir/ --query 'Contents[].Key' --output text)
	[ "$got" = "tree/dir/one.txt	tree/dir/sub/three.txt	tree/dir/two.txt" ] || fail list/prefix "got $got"
	pass list/prefix
	got=$(s3 list-objects-v2 --bucket demo --prefix tree/ --delimiter / --no-paginate \
		--query '{k:Contents[].Key,p:CommonPrefixes[].Prefix,n:KeyCount}' | jq -c .)
	[ "$got" = '{"k":["tree/top.txt"],"p":["tree/dir/"],"n":2}' ] || fail list/delimiter "got $got"
	pass list/delimiter

	mkdir -p many
	for i in $(seq -w 0 2499); do printf '%s' "$i" >"many/key-$i"; done
	aws --endpoint-url "$S3_ENDPOINT" s3 cp --recursive --quiet many s3://demo/many/ ||
		fail list/paginate "uploading 2,500 keys failed"
	local token="" pages=0 page
	: >keys.txt
	while :; do
		if [ -z "$token" ]; then
			page=$(s3 list-objects-v2 --bucket demo --prefix many/ --no-paginate --max-keys 500)
		else
			page=$(s3 list-objects-v2 --bucket demo --prefix many/ --no-paginate --max-keys 500 --continuation-token "$token")
		fi
		jq -r '.Contents[].Key' <<<"$page" >>keys.txt
		pages=$((pages + 1))
		[ "$(jq -r .IsTruncated <<<"$page")" = true ] || break
		token=$(jq -r .NextContinuationToken <<<"$page")
		[ "$pages" -lt 10 ] || fail list/paginate "paging does not end"
	done
	[ "$pages" -eq 5 ] || fail list/paginate "$pages pages, want 5"
	[ "$(wc -l <keys.txt | tr -d ' ')" -eq 2500 ] || fail list/paginate "$(wc -l <keys.txt) keys, want 2500"
	LC_ALL=C sort -c -u keys.txt 2>/dev/null || fail list/paginate "keys not in order or not distinct"
	got=$(s3 list-objects-v2 --bucket demo --prefix many/ --page-size 100 --query 'length(Contents)')
	[ "$got" = 2500 ] || fail list/paginate "the CLI's paginator returned $got keys"
	pass list/paginate
}

group_delete() {
	s3 delete-object --bucket demo --key small.txt >/dev/null || fail delete/twice "first delete failed"
	s3 delete-object --bucket demo --key small.txt >/dev/null || fail delete/twice "second delete failed"
	expect_failure delete/twice "(404)" head-object --bucket demo --key small.txt
	pass delete/twice
}

# group_crash_upload starts 20 parallel uploads of distinct 32 MiB files and
# records an ack for each one the CLI reports as stored. Ten use put-object
# with the default settings and finish quickly; ten use the CLI's s3 cp
# throttled to 4 MB/s (single part: tiny-s3 has no multipart upload), so they
# stay in flight for about 8 s. The host kills the server once the fast ones
# are acknowledged and the slow ones are sending.
group_crash_upload() {
	local i
	rm -rf crash acks
	mkdir -p crash acks
	for i in $(seq -w 1 20); do head -c 33554432 /dev/urandom >"crash/obj-$i.bin"; done
	cat >slow.cfg <<'CFG'
[default]
s3 =
  max_bandwidth = 4MB/s
  multipart_threshold = 1GB
CFG
	for i in $(seq -w 1 10); do
		(
			if s3 put-object --bucket demo --key "crash/obj-$i.bin" --body "crash/obj-$i.bin" >/dev/null 2>&1; then
				touch "acks/obj-$i.bin"
			fi
		) &
	done
	for i in $(seq -w 11 20); do
		(
			# No retries: these exist to be cut by the kill, and retrying
			# against a dead server only delays the restart.
			if AWS_CONFIG_FILE=$W/slow.cfg AWS_MAX_ATTEMPTS=1 aws --endpoint-url "$S3_ENDPOINT" s3 cp --quiet \
				"crash/obj-$i.bin" "s3://demo/crash/obj-$i.bin" >/dev/null 2>&1; then
				touch "acks/obj-$i.bin"
			fi
		) &
	done
	wait
	local acked=(acks/*)
	[ -e "${acked[0]}" ] || acked=()
	printf 'crash uploads: %s acknowledged\n' "${#acked[@]}"
}

# group_crash_verify runs after the restart: every acknowledged upload is
# listed, and every listed object equals its source and its ETag. It prints
# the number of objects in the bucket for the host's body count.
group_crash_verify() {
	local listed key etag name ack
	listed=$(s3 list-objects-v2 --bucket demo --prefix crash/ | jq -r '.Contents[]? | "\(.Key) \(.ETag)"')
	for ack in acks/*; do
		[ -e "$ack" ] || continue
		name=${ack#acks/}
		grep -q "^crash/$name " <<<"$listed" || fail crash/acknowledged "$name was acknowledged but is not listed"
	done
	pass crash/acknowledged
	while read -r key etag; do
		[ -n "$key" ] || continue
		s3 get-object --bucket demo --key "$key" out.bin >/dev/null || fail crash/etags "get-object $key failed"
		# The key is also the source file's path under /work.
		cmp -s "$key" out.bin || fail crash/etags "$key differs from its source"
		[ "$etag" = "\"$(md5sum out.bin | cut -d' ' -f1)\"" ] || fail crash/etags "$key: ETag $etag does not match its bytes"
	done <<<"$listed"
	pass crash/etags
	s3 list-objects-v2 --bucket demo --query 'length(Contents)'
}

case ${1:-} in
bucket) group_bucket ;;
object) group_object ;;
range) group_range ;;
list) group_list ;;
delete) group_delete ;;
crash-upload) group_crash_upload ;;
crash-verify) group_crash_verify ;;
*)
	echo "usage: checks.sh bucket|object|range|list|delete|crash-upload|crash-verify" >&2
	exit 2
	;;
esac
