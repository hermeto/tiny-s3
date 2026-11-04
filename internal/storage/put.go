package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

// copyBufferSize is the only per-Put body buffer.
const copyBufferSize = 32 << 10

// Put streams exactly size bytes from body into the store under bucket/key and
// returns the committed metadata. contentMD5 is the raw 16-byte digest the
// client sent, or nil. Put returns only after the body and the metadata record
// are durable.
func (s *Store) Put(ctx context.Context, bucket, key string, body io.Reader, size int64, contentMD5 []byte) (Info, error) {
	if err := validateKey(key); err != nil {
		return Info{}, err
	}
	if err := validateSize(size); err != nil {
		return Info{}, err
	}
	version := rand.Text()
	sum, err := s.writeData(ctx, body, size, contentMD5, s.dataPath(version))
	if err != nil {
		return Info{}, err
	}
	rec := record{
		V: recordVersion, Bucket: bucket, Key: key, Size: size,
		MD5: hex.EncodeToString(sum), LastModified: s.now().UTC(), Data: version,
	}
	tmp, err := s.writeRecord(rec)
	if err != nil {
		_ = s.fsys.Remove(s.dataPath(version))
		return Info{}, err
	}
	if err := s.commit(recordName(bucket, key), tmp, version); err != nil {
		return Info{}, err
	}
	return rec.info(), nil
}

// writeData streams the body into a temp file, checks its length and digest,
// syncs it, renames it to dst and syncs data/. It returns the body's MD5. On
// any error, every file it created is removed.
func (s *Store) writeData(ctx context.Context, body io.Reader, size int64, contentMD5 []byte, dst string) (sum []byte, err error) {
	f, err := s.fsys.CreateTemp(filepath.Join(s.root, tmpDir), "put-*")
	if err != nil {
		return nil, fmt.Errorf("storage: create temp: %w", err)
	}
	closed := false
	defer func() {
		if err == nil {
			return
		}
		if !closed {
			_ = f.Close()
		}
		// The original error matters more than a failed cleanup.
		_ = s.fsys.Remove(f.Name())
	}()

	h := md5.New()
	if err := copyBody(io.MultiWriter(f, h), &ctxReader{ctx: ctx, r: body}, size); err != nil {
		return nil, err
	}
	sum = h.Sum(nil)
	if contentMD5 != nil && !bytes.Equal(sum, contentMD5) {
		return nil, ErrBadDigest
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("storage: sync %s: %w", f.Name(), err)
	}
	closed = true
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("storage: close %s: %w", f.Name(), err)
	}
	if err := s.fsys.Rename(f.Name(), dst); err != nil {
		return nil, fmt.Errorf("storage: rename into data: %w", err)
	}
	if err := s.syncDir(dataDir); err != nil {
		_ = s.fsys.Remove(dst)
		return nil, err
	}
	return sum, nil
}

// copyBody copies exactly size bytes from r to w through one fixed buffer and
// then probes for one more byte, so a body shorter or longer than declared is
// ErrIncompleteBody. w is an io.MultiWriter, which has no ReadFrom, so
// io.CopyBuffer always uses buf.
func copyBody(w io.Writer, r io.Reader, size int64) error {
	buf := make([]byte, copyBufferSize)
	n, err := io.CopyBuffer(w, io.LimitReader(r, size), buf)
	if err != nil {
		return fmt.Errorf("storage: copy body: %w", err)
	}
	if n < size {
		return ErrIncompleteBody
	}
	m, err := io.ReadFull(r, buf[:1])
	switch {
	case m > 0:
		return ErrIncompleteBody
	case err != io.EOF:
		return fmt.Errorf("storage: read body: %w", err)
	}
	return nil
}

// ctxReader stops reading once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// writeRecord writes rec to a synced temp file and returns its path. On any
// error the temp file is removed.
func (s *Store) writeRecord(rec record) (path string, err error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("storage: encode record: %w", err)
	}
	f, err := s.fsys.CreateTemp(filepath.Join(s.root, tmpDir), "meta-*")
	if err != nil {
		return "", fmt.Errorf("storage: create temp record: %w", err)
	}
	closed := false
	defer func() {
		if err == nil {
			return
		}
		if !closed {
			_ = f.Close()
		}
		_ = s.fsys.Remove(f.Name())
	}()
	if _, err := f.Write(b); err != nil {
		return "", fmt.Errorf("storage: write record: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("storage: sync %s: %w", f.Name(), err)
	}
	closed = true
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("storage: close %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}

// commit makes the temp record tmp, which points at data version, the
// committed record of name. The rename into meta/ is the commit point and the
// meta/ sync makes it durable; only then is the previous version's data
// removed, best effort. On failure the new version is not left visible: a
// failed meta/ sync after the rename rolls the record back. The new data file
// is removed whenever no record points at it any more.
func (s *Store) commit(name, tmp, version string) (err error) {
	unlock := s.locks.lock(name)
	defer unlock()
	newData := s.dataPath(version)

	prev, err := s.readRecord(name)
	switch {
	case err == nil:
	case errors.Is(err, ErrNoSuchKey), errors.Is(err, ErrCorruptRecord):
		// Nothing to restore or clean up; a corrupt record is replaced.
		prev = record{}
	default:
		return errors.Join(err, s.fsys.Remove(tmp), s.fsys.Remove(newData))
	}

	if err := s.fsys.Rename(tmp, s.recordPath(name)); err != nil {
		_ = s.fsys.Remove(tmp)
		_ = s.fsys.Remove(newData)
		return fmt.Errorf("storage: rename record: %w", err)
	}
	if err := s.syncDir(metaDir); err != nil {
		if rbErr := s.rollback(name, prev); rbErr != nil {
			// The record may still point at the new data, so keep it: the
			// object is then the complete new version, never a partial one.
			return errors.Join(err, rbErr)
		}
		_ = s.fsys.Remove(newData)
		return err
	}
	if prev.Data != "" {
		_ = s.fsys.Remove(s.dataPath(prev.Data))
	}
	return nil
}

// rollback restores the committed state before a commit whose meta/ sync
// failed: it re-commits prev, or removes the record when there was none.
func (s *Store) rollback(name string, prev record) error {
	path := s.recordPath(name)
	if prev.Data == "" {
		if err := s.fsys.Remove(path); err != nil {
			return fmt.Errorf("storage: rollback: remove record: %w", err)
		}
		return s.syncDir(metaDir)
	}
	tmp, err := s.writeRecord(prev)
	if err != nil {
		return fmt.Errorf("storage: rollback: %w", err)
	}
	if err := s.fsys.Rename(tmp, path); err != nil {
		_ = s.fsys.Remove(tmp)
		return fmt.Errorf("storage: rollback: rename record: %w", err)
	}
	return s.syncDir(metaDir)
}

// syncDir fsyncs root/name so renames and removals in it are durable.
func (s *Store) syncDir(name string) error {
	path := filepath.Join(s.root, name)
	d, err := s.fsys.OpenDir(path)
	if err != nil {
		return fmt.Errorf("storage: open %s: %w", path, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("storage: sync %s: %w", path, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("storage: close %s: %w", path, err)
	}
	return nil
}
