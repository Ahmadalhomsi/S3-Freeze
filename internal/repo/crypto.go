package repo

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

// Blobs are encrypted with AES-256-GCM in fixed-size chunks (the STREAM
// construction): nonce = 7-byte random prefix | 4-byte counter | 1-byte
// last-chunk flag. This authenticates chunk order and detects truncation.

const (
	chunkSize   = 64 << 10
	prefixSize  = 7
	tagSize     = 16
	keyWrapAAD  = "s3sync-repo-key"
	kdfArgon2id = "argon2id"
)

var errDecrypt = errors.New("decryption failed: wrong passphrase or corrupted data")

type keys struct {
	enc [32]byte // AES-256 data key
	mac [32]byte // HMAC key for content-addressed blob IDs
}

func newKeys() (*keys, error) {
	k := &keys{}
	if _, err := rand.Read(k.enc[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(k.mac[:]); err != nil {
		return nil, err
	}
	return k, nil
}

type encConfig struct {
	KDF     string `json:"kdf"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	Nonce   []byte `json:"nonce"`
	Key     []byte `json:"key"`
}

func (c *encConfig) kek(passphrase string) []byte {
	return argon2.IDKey([]byte(passphrase), c.Salt, c.Time, c.Memory, c.Threads, 32)
}

func wrapKeys(k *keys, passphrase string) (*encConfig, error) {
	c := &encConfig{KDF: kdfArgon2id, Salt: make([]byte, 16), Time: 3, Memory: 64 * 1024, Threads: 4, Nonce: make([]byte, 12)}
	if _, err := rand.Read(c.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(c.Nonce); err != nil {
		return nil, err
	}
	aead, err := newGCM(c.kek(passphrase))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, 0, 64)
	plain = append(plain, k.enc[:]...)
	plain = append(plain, k.mac[:]...)
	c.Key = aead.Seal(nil, c.Nonce, plain, []byte(keyWrapAAD))
	return c, nil
}

func (c *encConfig) unwrap(passphrase string) (*keys, error) {
	if c.KDF != kdfArgon2id {
		return nil, errors.New("unsupported key derivation function " + c.KDF)
	}
	aead, err := newGCM(c.kek(passphrase))
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, c.Nonce, c.Key, []byte(keyWrapAAD))
	if err != nil || len(plain) != 64 {
		return nil, errors.New("wrong passphrase for this repository")
	}
	k := &keys{}
	copy(k.enc[:], plain[:32])
	copy(k.mac[:], plain[32:])
	return k, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(prefix []byte, counter uint32, last bool) []byte {
	n := make([]byte, 12)
	copy(n, prefix)
	binary.BigEndian.PutUint32(n[prefixSize:prefixSize+4], counter)
	if last {
		n[11] = 1
	}
	return n
}

type encWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	prefix  []byte
	counter uint32
	buf     []byte
	out     []byte
	closed  bool
}

func newEncWriter(w io.Writer, key []byte) (*encWriter, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, prefixSize)
	if _, err := rand.Read(prefix); err != nil {
		return nil, err
	}
	if _, err := w.Write(prefix); err != nil {
		return nil, err
	}
	return &encWriter{w: w, aead: aead, prefix: prefix, buf: make([]byte, 0, chunkSize)}, nil
}

func (e *encWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		// Only seal a full chunk once more data arrives, so the final chunk is
		// always sealed by Close with the last-chunk flag.
		if len(e.buf) == chunkSize {
			if err := e.seal(false); err != nil {
				return 0, err
			}
		}
		n := copy(e.buf[len(e.buf):chunkSize], p)
		e.buf = e.buf[:len(e.buf)+n]
		p = p[n:]
	}
	return total, nil
}

func (e *encWriter) seal(last bool) error {
	e.out = e.aead.Seal(e.out[:0], chunkNonce(e.prefix, e.counter, last), e.buf, nil)
	if _, err := e.w.Write(e.out); err != nil {
		return err
	}
	e.counter++
	if e.counter == 0 {
		return errors.New("blob too large to encrypt")
	}
	e.buf = e.buf[:0]
	return nil
}

func (e *encWriter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	return e.seal(true)
}

type decReader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	prefix  []byte
	counter uint32
	in      []byte
	plain   []byte
	pos     int
	done    bool
}

func newDecReader(r io.Reader, key []byte) (*decReader, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, prefixSize)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return nil, errDecrypt
	}
	return &decReader{
		r:      bufio.NewReaderSize(r, chunkSize+tagSize),
		aead:   aead,
		prefix: prefix,
		in:     make([]byte, chunkSize+tagSize),
	}, nil
}

func (d *decReader) Read(p []byte) (int, error) {
	for d.pos >= len(d.plain) {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain[d.pos:])
	d.pos += n
	return n, nil
}

func (d *decReader) next() error {
	n, err := io.ReadFull(d.r, d.in)
	last := false
	switch {
	case err == io.EOF || err == io.ErrUnexpectedEOF:
		last = true
	case err != nil:
		return err
	default:
		if _, perr := d.r.Peek(1); perr == io.EOF {
			last = true
		} else if perr != nil {
			return perr
		}
	}
	if n < tagSize {
		return errDecrypt
	}
	plain, err := d.aead.Open(d.plain[:0], chunkNonce(d.prefix, d.counter, last), d.in[:n], nil)
	if err != nil {
		return errDecrypt
	}
	d.plain, d.pos = plain, 0
	d.counter++
	d.done = last
	return nil
}
