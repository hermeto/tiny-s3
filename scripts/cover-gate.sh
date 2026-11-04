#!/usr/bin/env bash
# Coverage gate: every package under internal/ must have tests and 100%
# statement coverage (constitution Principle IV). Runs inside the Go
# container; see `make cover`.
#
# Coverage is computed from the profile as integer statement counts, never
# from the rounded percentage that `go test -cover` prints, so a single
# uncovered statement always fails the gate.
set -euo pipefail

profile=coverage.out
trap 'rm -f "$profile"' EXIT

mapfile -t pkgs < <(go list ./internal/...)
if [[ ${#pkgs[@]} -eq 0 ]]; then
	echo "coverage gate: FAILED (no packages under internal/)" >&2
	exit 1
fi

# Since Go 1.22, packages without tests still appear in the profile (with
# zero hits), so detect them from their file lists instead.
declare -A notest=()
while read -r pkg; do
	[[ -n $pkg ]] && notest[$pkg]=1
done < <(go list -f '{{if not (or .TestGoFiles .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./internal/...)

if ! go test -covermode=atomic -coverprofile="$profile" ./internal/...; then
	echo "coverage gate: FAILED (tests failed)"
	exit 1
fi

# Profile lines: <import path>/<file>.go:<start>,<end> <statements> <count>
# A block may repeat if several test binaries cover it; count it once and
# treat it as covered if any binary hit it.
declare -A covered=() total=()
while read -r pkg c t; do
	covered[$pkg]=$c
	total[$pkg]=$t
done < <(awk '
	NR == 1 && /^mode:/ { next }
	{
		block = $1
		if (!(block in stmts)) {
			file = block
			sub(/:.*$/, "", file)
			sub(/\/[^\/]*$/, "", file)
			stmts[block] = $2
			pkgof[block] = file
		}
		if ($3 > 0) hit[block] = 1
	}
	END {
		for (b in stmts) {
			p = pkgof[b]
			tot[p] += stmts[b]
			if (b in hit) cov[p] += stmts[b]
		}
		for (p in tot) printf "%s %d %d\n", p, cov[p] + 0, tot[p]
	}
' "$profile")

failed=0
echo
printf '%-50s %13s %8s\n' PACKAGE COVERED/TOTAL PERCENT
for pkg in "${pkgs[@]}"; do
	c=${covered[$pkg]:-0}
	t=${total[$pkg]:-0}
	pct=$(awk -v c="$c" -v t="$t" 'BEGIN { if (t == 0) print "100.0%"; else printf "%.1f%%\n", 100 * c / t }')
	status=""
	if [[ -n ${notest[$pkg]:-} ]]; then
		status="  FAIL (no tests)"
		failed=$((failed + 1))
	elif ((c != t)); then
		status="  FAIL"
		failed=$((failed + 1))
	fi
	printf '%-50s %13s %8s%s\n' "$pkg" "$c/$t" "$pct" "$status"
done

if ((failed > 0)); then
	noun=packages
	((failed == 1)) && noun=package
	echo "coverage gate: FAILED ($failed $noun below 100%)"
	exit 1
fi
echo "coverage gate: PASSED"
