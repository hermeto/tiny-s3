package awschunked

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

const sig = ";chunk-signature=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// decode reads all of stream through a Reader using reads of n bytes.
func decode(stream string, n int) (string, *Reader, error) {
	r := NewReader(strings.NewReader(stream))
	var out strings.Builder
	buf := make([]byte, n)
	for {
		m, err := r.Read(buf)
		out.Write(buf[:m])
		if err == io.EOF {
			return out.String(), r, nil
		}
		if err != nil {
			return out.String(), r, err
		}
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stream, want string
		trailers     map[string]string
	}{
		"captured cli": {"b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:jSnVw/bqjr4=\r\n\r\n", "hello world",
			map[string]string{"x-amz-checksum-crc64nvme": "jSnVw/bqjr4="}},
		"several chunks":     {"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n", "hello world", nil},
		"upper-case hex":     {"A\r\n0123456789\r\n0\r\n\r\n", "0123456789", nil},
		"empty object":       {"0\r\n\r\n", "", nil},
		"empty with trailer": {"0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n", "", map[string]string{"x-amz-checksum-crc32": "AAAAAA=="}},
		"signed":             {"5" + sig + "\r\nhello\r\n6" + sig + "\r\n world\r\n0" + sig + "\r\n\r\n", "hello world", nil},
		"signed trailer": {"b" + sig + "\r\nhello world\r\n0" + sig + "\r\nx-amz-checksum-crc32:DUoRhQ==\r\nx-amz-trailer-signature:abc\r\n\r\n",
			"hello world", map[string]string{"x-amz-checksum-crc32": "DUoRhQ==", "x-amz-trailer-signature": "abc"}},
		"trailer spaces": {"0\r\nX-Amz-Checksum-SHA1 : abc \r\n\r\n", "", map[string]string{"x-amz-checksum-sha1": "abc"}},
	}
	for name, tt := range tests {
		for _, n := range []int{1, 7, 4096} {
			got, r, err := decode(tt.stream, n)
			if err != nil || got != tt.want {
				t.Fatalf("%s/%d: decode = %q, %v; want %q", name, n, got, err, tt.want)
			}
			for k, v := range tt.trailers {
				if got, ok := r.Trailer(strings.ToUpper(k)); !ok || got != v {
					t.Fatalf("%s: Trailer(%s) = %q, %v; want %q", name, k, got, ok, v)
				}
			}
			if _, ok := r.Trailer("x-amz-missing"); ok {
				t.Fatalf("%s: Trailer(missing) found", name)
			}
		}
	}
}

func TestTrailerBeforeEOF(t *testing.T) {
	t.Parallel()
	r := NewReader(strings.NewReader("5\r\nhello\r\n0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n"))
	if _, err := r.Read(make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Trailer("x-amz-checksum-crc32"); ok {
		t.Fatal("Trailer available before io.EOF")
	}
	// Reads after EOF keep returning EOF.
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("Read after EOF = %d, %v", n, err)
	}
}

func TestMalformed(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"bad hex":            "zz\r\nhello\r\n0\r\n\r\n",
		"empty size":         "\r\nhello\r\n0\r\n\r\n",
		"signed empty size":  sig + "\r\nhello\r\n0\r\n\r\n",
		"size over 63 bits":  "8000000000000000\r\n",
		"no crlf after data": "5\r\nhelloXX0\r\n\r\n",
		"bare lf header":     "5\nhello\r\n0\r\n\r\n",
		"bare lf trailer":    "0\r\nx-amz-checksum-crc32:AAAAAA==\n\r\n",
		"long line":          strings.Repeat("0", 4095) + "5\r\nhello\r\n0\r\n\r\n",
		"trailer no colon":   "0\r\nnot a trailer\r\n\r\n",
		"too many trailers":  "0\r\n" + strings.Repeat("a:b\r\n", 17) + "\r\n",
		"bytes after end":    "0\r\n\r\nextra",
		"negative size":      "-1\r\n",
	}
	for name, stream := range tests {
		if _, _, err := decode(stream, 4096); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
	// A line of exactly 4096 bytes including CRLF is accepted.
	line := strings.Repeat("0", 4094-1) + "5"
	if got, _, err := decode(line+"\r\nhello\r\n0\r\n\r\n", 4096); err != nil || got != "hello" {
		t.Errorf("4096-byte line: %q, %v", got, err)
	}
	// Sixteen trailers are accepted.
	if _, _, err := decode("0\r\n"+strings.Repeat("a:b\r\n", 16)+"\r\n", 4096); err != nil {
		t.Errorf("16 trailers: %v", err)
	}
}

func TestTruncated(t *testing.T) {
	t.Parallel()
	full := "5\r\nhello\r\n0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n"
	for _, cut := range []int{1, 2, 4, 8, 10, 12, 20, len(full) - 2, len(full) - 1} {
		if _, _, err := decode(full[:cut], 4096); !errors.Is(err, ErrTruncated) {
			t.Errorf("cut at %d (%q): err = %v, want ErrTruncated", cut, full[:cut], err)
		}
	}
	if _, _, err := decode("", 4096); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty stream: err = %v, want ErrTruncated", err)
	}
	// net/http reports a cut HTTP chunked body as io.ErrUnexpectedEOF.
	r := NewReader(io.MultiReader(strings.NewReader("5\r\nhel"), iotest.ErrReader(io.ErrUnexpectedEOF)))
	if _, err := io.ReadAll(r); !errors.Is(err, ErrTruncated) {
		t.Errorf("unexpected EOF: err = %v, want ErrTruncated", err)
	}
}

func TestUnderlyingErrorPassesThrough(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, prefix := range []string{"", "5\r\nhel"} {
		r := NewReader(io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(boom)))
		if _, err := io.ReadAll(r); !errors.Is(err, boom) || errors.Is(err, ErrTruncated) || errors.Is(err, ErrMalformed) {
			t.Errorf("prefix %q: err = %v, want boom", prefix, err)
		}
	}
}

func TestEmptyReadAndErrorAfterEnd(t *testing.T) {
	t.Parallel()
	r := NewReader(strings.NewReader("5\r\nhello\r\n0\r\n\r\n"))
	if _, err := r.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("Read(nil) mid-chunk = %d, %v", n, err)
	}

	// An error from the connection after the final line is not hidden.
	boom := errors.New("boom")
	r = NewReader(io.MultiReader(strings.NewReader("0\r\n\r\n"), iotest.ErrReader(boom)))
	if _, err := io.ReadAll(r); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
