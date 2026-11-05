package awschunked

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
)

// zeros yields n zero bytes without holding them.
type zeros struct{ n int64 }

func (z *zeros) Read(p []byte) (int, error) {
	if z.n == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > z.n {
		p = p[:z.n]
	}
	clear(p)
	z.n -= int64(len(p))
	return len(p), nil
}

func TestDecodeSingleHugeChunkBoundedMemory(t *testing.T) {
	const size = 64 << 20
	stream := io.MultiReader(
		strings.NewReader(fmt.Sprintf("%x\r\n", size)),
		&zeros{n: size},
		strings.NewReader("\r\n0\r\n\r\n"),
	)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	n, err := io.CopyBuffer(io.Discard, NewReader(stream), make([]byte, 32<<10))
	runtime.ReadMemStats(&after)
	if err != nil || n != size {
		t.Fatalf("decoded %d bytes, %v", n, err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= 4<<20 {
		t.Fatalf("decoding allocated %d bytes, want < 4 MiB", grew)
	}
}
