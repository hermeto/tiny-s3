package s3api

import (
	"net/http"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

func TestParseRange(t *testing.T) {
	t.Parallel()
	serve := func(first, last int64) byteRange { return byteRange{kind: rangeServe, first: first, last: last} }
	ignore := byteRange{kind: rangeIgnore}
	unsat := byteRange{kind: rangeUnsatisfiable}
	tests := []struct {
		header string
		size   int64
		want   byteRange
	}{
		{"", 1000, ignore},
		{"bytes=0-99", 1000, serve(0, 99)},
		{"bytes=100-", 1000, serve(100, 999)},
		{"bytes=-50", 1000, serve(950, 999)},
		{"bytes=990-5000", 1000, serve(990, 999)},
		{"bytes=-5000", 1000, serve(0, 999)},
		{"bytes=0-", 1000, serve(0, 999)},
		{"bytes=999-999", 1000, serve(999, 999)},
		{"BYTES=0-9", 1000, serve(0, 9)},
		{" bytes = 0 - 9 ", 1000, serve(0, 9)},
		{"bytes=1000-", 1000, unsat},
		{"bytes=1000-2000", 1000, unsat},
		{"bytes=-0", 1000, unsat},
		{"bytes=0-0", 0, unsat},
		{"bytes=-5", 0, unsat},
		{"bytes=0-", 0, unsat},
		{"bytes=0-1,5-6", 1000, ignore},
		{"bytes=0-1,", 1000, ignore},
		{"bytes=abc", 1000, ignore},
		{"bytes=a-9", 1000, ignore},
		{"bytes=0-b", 1000, ignore},
		{"items=0-1", 1000, ignore},
		{"bytes0-1", 1000, ignore},
		{"bytes=5-3", 1000, ignore},
		{"bytes=-", 1000, ignore},
		{"bytes=", 1000, ignore},
		{"bytes=0", 1000, ignore},
		{"bytes=99999999999999999999-", 1000, ignore},
		{"bytes=0-99999999999999999999", 1000, ignore},
		{"bytes=-99999999999999999999", 1000, ignore},
		{"bytes=+1-2", 1000, ignore},
	}
	for _, tt := range tests {
		if got := parseRange(tt.header, tt.size); got != tt.want {
			t.Errorf("parseRange(%q, %d) = %+v, want %+v", tt.header, tt.size, got, tt.want)
		}
	}
}

func TestIfRangeMatches(t *testing.T) {
	t.Parallel()
	modified := time.Date(2026, 11, 4, 14, 30, 15, 900_000_000, time.UTC)
	info := storage.Info{MD5: "5eb63bbbe01eeed093cb22bb8f5acdc3", LastModified: modified}
	tests := map[string]bool{
		`"5eb63bbbe01eeed093cb22bb8f5acdc3"`:                   true,
		"5eb63bbbe01eeed093cb22bb8f5acdc3":                     true,
		`"00000000000000000000000000000000"`:                   false,
		`W/"5eb63bbbe01eeed093cb22bb8f5acdc3"`:                 false,
		"garbage":                                              false,
		modified.Truncate(time.Second).Format(http.TimeFormat): true,
		modified.Add(time.Second).Format(http.TimeFormat):      false,
		modified.Add(-time.Second).Format(http.TimeFormat):     false,
	}
	for v, want := range tests {
		if got := ifRangeMatches(v, info); got != want {
			t.Errorf("ifRangeMatches(%q) = %v, want %v", v, got, want)
		}
	}
}
