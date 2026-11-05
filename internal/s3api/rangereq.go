package s3api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// rangeKind is what a Range header asks for.
type rangeKind int

const (
	// rangeIgnore serves the whole object: no header, a malformed one, or
	// several ranges (S3 does not answer multipart/byteranges).
	rangeIgnore rangeKind = iota
	// rangeServe serves first..last as 206 Partial Content.
	rangeServe
	// rangeUnsatisfiable answers 416 InvalidRange.
	rangeUnsatisfiable
)

// byteRange is a parsed Range header; first and last are inclusive and only
// set for rangeServe.
type byteRange struct {
	kind        rangeKind
	first, last int64
}

// parseRange applies S3's rules for a Range header on an object of size
// bytes: one range of the form first-last, first- or -suffix, the unit
// "bytes" case-insensitively, spaces around the value and the dash allowed.
func parseRange(header string, size int64) byteRange {
	unit, spec, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(unit), "bytes") || strings.Contains(spec, ",") {
		return byteRange{}
	}
	a, b, ok := strings.Cut(spec, "-")
	if !ok {
		return byteRange{}
	}
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	unsatisfiable := byteRange{kind: rangeUnsatisfiable}
	if a == "" {
		suffix, ok := parseUint(b)
		switch {
		case !ok:
			return byteRange{}
		case suffix == 0 || size == 0:
			return unsatisfiable
		}
		return byteRange{kind: rangeServe, first: size - min(suffix, size), last: size - 1}
	}
	first, ok := parseUint(a)
	if !ok {
		return byteRange{}
	}
	last := size - 1
	if b != "" {
		end, ok := parseUint(b)
		if !ok || first > end {
			return byteRange{}
		}
		last = min(end, size-1)
	}
	if first >= size {
		return unsatisfiable
	}
	return byteRange{kind: rangeServe, first: first, last: last}
}

// parseUint parses a non-empty run of decimal digits that fits in 63 bits.
func parseUint(s string) (int64, bool) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// ifRangeMatches reports whether an If-Range value names the current object:
// its strong ETag (quoted or not) or its last-modified time in whole seconds.
// A weak ETag never matches.
func ifRangeMatches(v string, info storage.Info) bool {
	if strings.HasPrefix(v, "W/") {
		return false
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.Equal(info.LastModified.UTC().Truncate(time.Second))
	}
	return strings.Trim(v, `"`) == info.MD5
}
