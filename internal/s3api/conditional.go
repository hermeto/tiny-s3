package s3api

import (
	"net/http"
	"strings"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// evaluateConditions returns the status the conditional headers of h call
// for on info: 200 to serve the object, 412 or 304. The order is the HTTP
// one S3 follows: If-Match, or If-Unmodified-Since without it; then
// If-None-Match, or If-Modified-Since without it. Dates are compared in
// whole seconds, the precision of Last-Modified; unparseable dates are
// ignored.
func evaluateConditions(h http.Header, info storage.Info) int {
	modified := info.LastModified.UTC().Truncate(time.Second)
	if v := h.Get("If-Match"); v != "" {
		if !etagListMatches(v, info.MD5) {
			return http.StatusPreconditionFailed
		}
	} else if t, ok := parseDate(h.Get("If-Unmodified-Since")); ok && modified.After(t) {
		return http.StatusPreconditionFailed
	}
	if v := h.Get("If-None-Match"); v != "" {
		if etagListMatches(v, info.MD5) {
			return http.StatusNotModified
		}
	} else if t, ok := parseDate(h.Get("If-Modified-Since")); ok && !modified.After(t) {
		return http.StatusNotModified
	}
	return http.StatusOK
}

// etagListMatches reports whether a comma-separated list of ETags names the
// object with hex MD5 md5. "*" matches any object; quotes and the weak
// prefix are ignored.
func etagListMatches(list, md5 string) bool {
	for _, e := range strings.Split(list, ",") {
		e = strings.TrimSpace(e)
		if e == "*" {
			return true
		}
		e = strings.Trim(strings.TrimPrefix(e, "W/"), `"`)
		if e == md5 {
			return true
		}
	}
	return false
}

func parseDate(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	t, err := http.ParseTime(v)
	return t, err == nil
}

// writeConditional answers a failed condition: 412 as an S3 error, or 304
// with the validators and no body.
func writeConditional(w http.ResponseWriter, r *http.Request, status int, info storage.Info) {
	if status == http.StatusPreconditionFailed {
		WriteError(w, r, ErrPreconditionFailed)
		return
	}
	w.Header().Set("ETag", `"`+info.MD5+`"`)
	w.Header().Set("Last-Modified", lastModified(info))
	w.WriteHeader(http.StatusNotModified)
}
