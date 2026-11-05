package storage

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// listKeys returns the keys of a listing result.
func listKeys(r ListResult) []string {
	keys := make([]string, 0, len(r.Objects))
	for _, o := range r.Objects {
		keys = append(keys, o.Key)
	}
	return keys
}

// storeWith puts each key (body = key) into bucket b of a fresh store.
func storeWith(t *testing.T, keys ...string) *Store {
	t.Helper()
	s, _ := newStore(t, nil)
	for _, k := range keys {
		put(t, s, "b", k, []byte(k))
	}
	return s
}

func TestListAll(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	for i, k := range []string{"z", "a/b", "é", "a"} {
		s.now = func() time.Time { return fixedTime.Add(time.Duration(i) * time.Second) }
		put(t, s, "b", k, []byte(k+"!"))
	}
	r := s.List("b", ListOptions{MaxKeys: 1000})
	if got := listKeys(r); !slices.Equal(got, []string{"a", "a/b", "z", "é"}) || r.Truncated || len(r.CommonPrefixes) != 0 {
		t.Fatalf("List = %+v", r)
	}
	for _, o := range r.Objects {
		info, err := s.Head("b", o.Key)
		if err != nil || o.Size != info.Size || o.MD5 != info.MD5 || !o.LastModified.Equal(info.LastModified) {
			t.Fatalf("%s: summary %+v, Head %+v (%v)", o.Key, o, info, err)
		}
	}

	// The listing itself touches no file (the Head calls above did).
	fsys := fsx.NewFaulty(fsx.OS{})
	s.fsys = fsys
	s.List("b", ListOptions{MaxKeys: 1000})
	if h := fsys.History(); len(h) != 0 {
		t.Fatalf("List made filesystem calls: %v", h)
	}
}

func TestListEmpty(t *testing.T) {
	t.Parallel()
	s := storeWith(t, "a", "b")
	for name, r := range map[string]ListResult{
		"unknown bucket": s.List("other", ListOptions{MaxKeys: 1000}),
		"max keys 0":     s.List("b", ListOptions{MaxKeys: 0}),
	} {
		if len(r.Objects) != 0 || len(r.CommonPrefixes) != 0 || r.Truncated {
			t.Fatalf("%s: %+v, want an empty, complete result", name, r)
		}
	}
}

func TestListPrefixAndDelimiter(t *testing.T) {
	t.Parallel()
	s := storeWith(t, "a/1", "a/2", "b/1", "dir/", "dir/f1", "dir/sub/f2", "dir/sub/f3", "dir/zz/x", "dirt", "top", "x--y--z", "x--w", "xa")
	tests := []struct {
		name     string
		opts     ListOptions
		keys     []string
		prefixes []string
		trunc    bool
	}{
		{"prefix", ListOptions{Prefix: "a/"}, []string{"a/1", "a/2"}, nil, false},
		{"prefix without match", ListOptions{Prefix: "nope"}, nil, nil, false},
		{"prefix past every key", ListOptions{Prefix: "~"}, nil, nil, false},
		{"delimiter at the top", ListOptions{Delimiter: "/"}, []string{"dirt", "top", "x--w", "x--y--z", "xa"}, []string{"a/", "b/", "dir/"}, false},
		{"prefix and delimiter", ListOptions{Prefix: "dir/", Delimiter: "/"}, []string{"dir/", "dir/f1"}, []string{"dir/sub/", "dir/zz/"}, false},
		{"multi-character delimiter", ListOptions{Prefix: "x", Delimiter: "--"}, []string{"xa"}, []string{"x--"}, false},
		{"multi-character delimiter, deeper", ListOptions{Prefix: "x--", Delimiter: "--"}, []string{"x--w"}, []string{"x--y--"}, false},
		{"prefixes count towards max keys", ListOptions{Delimiter: "/", MaxKeys: 2}, nil, []string{"a/", "b/"}, true},
		{"page ends on a key", ListOptions{Prefix: "dir/", Delimiter: "/", MaxKeys: 3}, []string{"dir/", "dir/f1"}, []string{"dir/sub/"}, true},
	}
	for _, tt := range tests {
		if tt.opts.MaxKeys == 0 {
			tt.opts.MaxKeys = 1000
		}
		r := s.List("b", tt.opts)
		if !slices.Equal(listKeys(r), tt.keys) && !(len(r.Objects) == 0 && len(tt.keys) == 0) {
			t.Errorf("%s: keys %q, want %q", tt.name, listKeys(r), tt.keys)
		}
		if !slices.Equal(r.CommonPrefixes, tt.prefixes) && !(len(r.CommonPrefixes) == 0 && len(tt.prefixes) == 0) {
			t.Errorf("%s: prefixes %q, want %q", tt.name, r.CommonPrefixes, tt.prefixes)
		}
		if r.Truncated != tt.trunc {
			t.Errorf("%s: truncated %v, want %v", tt.name, r.Truncated, tt.trunc)
		}
	}
}

func TestListPaging(t *testing.T) {
	t.Parallel()
	s := storeWith(t, "a", "b", "c", "d/1", "d/2", "d/3", "e")
	tests := []struct {
		name      string
		opts      ListOptions
		keys      []string
		prefixes  []string
		trunc     bool
		next      string
		nextGroup bool
	}{
		{"first page", ListOptions{MaxKeys: 2}, []string{"a", "b"}, nil, true, "b", false},
		{"after a key", ListOptions{After: "b", MaxKeys: 2}, []string{"c", "d/1"}, nil, true, "d/1", false},
		{"after a key that is not stored", ListOptions{After: "bb", MaxKeys: 1}, []string{"c"}, nil, true, "c", false},
		{"page ends on a group", ListOptions{After: "b", Delimiter: "/", MaxKeys: 2}, []string{"c"}, []string{"d/"}, true, "d/", true},
		{"after a whole group", ListOptions{After: "d/", AfterGroup: true, Delimiter: "/", MaxKeys: 2}, []string{"e"}, nil, false, "", false},
		{"after a group without delimiter", ListOptions{After: "d/", AfterGroup: true, MaxKeys: 5}, []string{"e"}, nil, false, "", false},
		{"after a key inside a group", ListOptions{After: "d/1", MaxKeys: 5}, []string{"d/2", "d/3", "e"}, nil, false, "", false},
		{"after a key before the prefix", ListOptions{After: "a", Prefix: "d/", MaxKeys: 5}, []string{"d/1", "d/2", "d/3"}, nil, false, "", false},
		{"after past every key", ListOptions{After: "z", MaxKeys: 5}, nil, nil, false, "", false},
		{"exactly max keys left", ListOptions{After: "d/3", MaxKeys: 1}, []string{"e"}, nil, false, "", false},
	}
	for _, tt := range tests {
		r := s.List("b", tt.opts)
		if !slices.Equal(listKeys(r), tt.keys) && !(len(r.Objects) == 0 && len(tt.keys) == 0) {
			t.Errorf("%s: keys %q, want %q", tt.name, listKeys(r), tt.keys)
		}
		if !slices.Equal(r.CommonPrefixes, tt.prefixes) && !(len(r.CommonPrefixes) == 0 && len(tt.prefixes) == 0) {
			t.Errorf("%s: prefixes %q, want %q", tt.name, r.CommonPrefixes, tt.prefixes)
		}
		if r.Truncated != tt.trunc || r.Next != tt.next || r.NextGroup != tt.nextGroup {
			t.Errorf("%s: truncated %v next %q group %v; want %v %q %v", tt.name, r.Truncated, r.Next, r.NextGroup, tt.trunc, tt.next, tt.nextGroup)
		}
	}
}

// TestListPagesEveryKeyOnce pages through every shape with the result's Next
// and checks each key or group appears exactly once.
func TestListPagesEveryKeyOnce(t *testing.T) {
	t.Parallel()
	var keys []string
	for i := range 250 {
		keys = append(keys, fmt.Sprintf("k%03d", i), fmt.Sprintf("g%02d/x%03d", i%20, i))
	}
	s := storeWith(t, keys...)
	for _, delim := range []string{"", "/"} {
		for _, max := range []int{1, 7, 100, 1000} {
			seen := map[string]int{}
			opts := ListOptions{Delimiter: delim, MaxKeys: max}
			for pages := 0; ; pages++ {
				if pages > len(keys)+1 {
					t.Fatalf("delimiter %q max %d: paging does not end", delim, max)
				}
				r := s.List("b", opts)
				for _, k := range listKeys(r) {
					seen[k]++
				}
				for _, p := range r.CommonPrefixes {
					seen[p]++
				}
				if !r.Truncated {
					break
				}
				opts.After, opts.AfterGroup = r.Next, r.NextGroup
			}
			want := len(keys)
			if delim == "/" {
				want = 250 + 20
			}
			if len(seen) != want {
				t.Fatalf("delimiter %q max %d: %d distinct entries, want %d", delim, max, len(seen), want)
			}
			for k, n := range seen {
				if n != 1 {
					t.Fatalf("delimiter %q max %d: %q listed %d times", delim, max, k, n)
				}
			}
		}
	}
}
