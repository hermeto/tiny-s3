package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

func TestReaderSurvivesOverwrite(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	old := bytes.Repeat([]byte("A"), 200_000)
	put(t, s, "b", "k", old)
	_, r, err := s.Get("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	head := make([]byte, 10)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatal(err)
	}
	put(t, s, "b", "k", []byte("new"))
	rest, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(append(head, rest...), old) {
		t.Fatalf("reader after overwrite: %d bytes, %v; want the original %d", len(head)+len(rest), err, len(old))
	}
	if _, b := readAll(t, s, "b", "k"); string(b) != "new" {
		t.Fatalf("new Get = %q", b)
	}
}

func TestReaderSurvivesDelete(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	old := bytes.Repeat([]byte("B"), 200_000)
	put(t, s, "b", "k", old)
	_, r, err := s.Get("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := s.Delete("b", "k"); err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(r); err != nil || !bytes.Equal(b, old) {
		t.Fatalf("reader after delete: %d bytes, %v", len(b), err)
	}
	if _, err := s.Head("b", "k"); !errors.Is(err, ErrNoSuchKey) {
		t.Fatalf("Head after delete = %v", err)
	}
}

// body returns the distinct body of writer i: its size and bytes differ.
func body(i int) []byte {
	return bytes.Repeat([]byte(fmt.Sprintf("writer-%02d|", i)), 1000+i*37)
}

func TestConcurrentPutsHaveOneWinner(t *testing.T) {
	t.Parallel()
	const writers = 16
	s, root := newStore(t, nil)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b := body(i)
			if _, err := s.Put(context.Background(), "b", "k", bytes.NewReader(b), int64(len(b)), nil); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			info, r, err := s.Get("b", "k")
			if errors.Is(err, ErrNoSuchKey) {
				return
			}
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			if err != nil || int64(len(b)) != info.Size || md5hex(b) != info.MD5 {
				t.Errorf("concurrent Get: %d bytes, %v; info %+v", len(b), err, info)
			}
		}()
	}
	wg.Wait()

	info, b := readAll(t, s, "b", "k")
	winners := 0
	for i := range writers {
		if bytes.Equal(b, body(i)) {
			winners++
		}
	}
	if winners != 1 || info.Size != int64(len(b)) || info.MD5 != md5hex(b) {
		t.Fatalf("final object matches %d writers; info %+v, %d bytes", winners, info, len(b))
	}
	if tmp, data := entries(t, root, "tmp"), entries(t, root, "data"); len(tmp) != 0 || len(data) != 1 {
		t.Fatalf("tmp %v, data %v; want empty tmp and one data file", tmp, data)
	}
}

func TestConcurrentPutsAndDeletes(t *testing.T) {
	t.Parallel()
	const workers = 16
	s, root := newStore(t, nil)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 1 {
				if err := s.Delete("b", "k"); err != nil {
					t.Errorf("Delete: %v", err)
				}
				return
			}
			b := body(i)
			if _, err := s.Put(context.Background(), "b", "k", bytes.NewReader(b), int64(len(b)), nil); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	data := entries(t, root, "data")
	if tmp := entries(t, root, "tmp"); len(tmp) != 0 {
		t.Fatalf("tmp = %v", tmp)
	}
	info, r, err := s.Get("b", "k")
	if errors.Is(err, ErrNoSuchKey) {
		if len(data) != 0 {
			t.Fatalf("key absent but data = %v", data)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || md5hex(b) != info.MD5 || len(data) != 1 {
		t.Fatalf("final: %+v, %d bytes, %v, data %v", info, len(b), err, data)
	}
}
