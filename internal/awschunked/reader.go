// Package awschunked decodes the aws-chunked framing AWS clients use for
// streaming uploads: size-prefixed chunks, optionally signed, ending with a
// zero-size chunk and optional trailers such as a checksum.
//
//	stream     = *chunk last-chunk *trailer CRLF
//	chunk      = hex-size [";" extension] CRLF data CRLF
//	last-chunk = "0" [";" extension] CRLF
//	trailer    = name ":" value CRLF
//
// Chunk extensions (chunk-signature) and trailer signatures are not
// verified. Every line is at most MaxLine bytes, so decoding needs one fixed
// buffer whatever the chunk sizes are.
package awschunked

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// MaxLine is the longest chunk-header or trailer line, CRLF included.
	MaxLine = 4096
	// MaxTrailers is the most trailers a stream may carry.
	MaxTrailers = 16
)

// Errors returned by Reader.
var (
	// ErrMalformed is a stream that does not follow the framing.
	ErrMalformed = errors.New("awschunked: malformed chunk stream")
	// ErrTruncated is a stream that ends before its final line.
	ErrTruncated = errors.New("awschunked: chunk stream ends early")
)

// Reader yields the decoded payload of an aws-chunked stream. It returns
// io.EOF only after the final empty line, and fails on any byte after it.
type Reader struct {
	br       *bufio.Reader
	left     int64 // payload bytes left in the current chunk
	needCRLF bool  // the current chunk's data is read; its CRLF is not
	done     bool  // the final empty line was read
	err      error // sticky error, io.EOF once done
	trailers map[string]string
	crlf     [2]byte
}

// NewReader returns a Reader decoding r.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, MaxLine)}
}

// Read reads decoded payload bytes.
func (r *Reader) Read(p []byte) (int, error) {
	for r.err == nil {
		if r.left > 0 {
			if len(p) == 0 {
				return 0, nil
			}
			if int64(len(p)) > r.left {
				p = p[:r.left]
			}
			n, err := r.br.Read(p)
			r.left -= int64(n)
			r.needCRLF = r.left == 0
			if err != nil {
				r.fail(err)
			}
			if n > 0 {
				return n, nil
			}
			continue
		}
		r.next()
	}
	return 0, r.err
}

// next consumes the end of the current chunk and the next chunk header, or
// the trailers and the end of the stream.
func (r *Reader) next() {
	if r.needCRLF {
		if _, err := io.ReadFull(r.br, r.crlf[:]); err != nil {
			r.fail(err)
			return
		}
		if string(r.crlf[:]) != "\r\n" {
			r.malformed("chunk data not followed by CRLF")
			return
		}
		r.needCRLF = false
	}
	line, ok := r.readLine()
	if !ok {
		return
	}
	sizeField, _, _ := strings.Cut(line, ";")
	size, err := strconv.ParseInt(sizeField, 16, 64)
	if err != nil || size < 0 {
		r.malformed(fmt.Sprintf("bad chunk size %q", sizeField))
		return
	}
	if size > 0 {
		r.left = size
		return
	}
	r.readTrailers()
}

// readTrailers reads the trailer lines up to the final empty line and checks
// that the stream ends there.
func (r *Reader) readTrailers() {
	trailers := make(map[string]string)
	for count := 0; ; count++ {
		line, ok := r.readLine()
		if !ok {
			return
		}
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			r.malformed("trailer without a colon")
			return
		}
		if count == MaxTrailers {
			r.malformed(fmt.Sprintf("more than %d trailers", MaxTrailers))
			return
		}
		trailers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	switch _, err := r.br.ReadByte(); err {
	case nil:
		r.malformed("bytes after the end of the stream")
	case io.EOF:
		r.trailers, r.done, r.err = trailers, true, io.EOF
	default:
		r.fail(err)
	}
}

// readLine returns the next line without its CRLF.
func (r *Reader) readLine() (string, bool) {
	line, err := r.br.ReadSlice('\n')
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		r.malformed(fmt.Sprintf("line longer than %d bytes", MaxLine))
		return "", false
	case err != nil:
		r.fail(err)
		return "", false
	}
	s, ok := strings.CutSuffix(string(line), "\r\n")
	if !ok {
		r.malformed("line not ended by CRLF")
		return "", false
	}
	return s, true
}

func (r *Reader) malformed(why string) {
	r.err = fmt.Errorf("%w: %s", ErrMalformed, why)
}

// fail records an error from the underlying reader: the end of the input
// before the final line is a truncation, anything else passes through.
func (r *Reader) fail(err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		r.err = ErrTruncated
		return
	}
	r.err = err
}

// Trailer returns the trailer name (case-insensitive) once Read has returned
// io.EOF.
func (r *Reader) Trailer(name string) (string, bool) {
	if !r.done {
		return "", false
	}
	v, ok := r.trailers[strings.ToLower(name)]
	return v, ok
}
