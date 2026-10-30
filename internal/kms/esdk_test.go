package kms

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

// esdkReader walks a message, failing the test on a short read.
type esdkReader struct {
	t   *testing.T
	b   []byte
	off int
}

func (r *esdkReader) next(n int) []byte {
	r.t.Helper()
	if r.off+n > len(r.b) {
		r.t.Fatalf("message truncated at offset %d (want %d more bytes)", r.off, n)
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *esdkReader) u8() int     { return int(r.next(1)[0]) }
func (r *esdkReader) u16() int    { return int(binary.BigEndian.Uint16(r.next(2))) }
func (r *esdkReader) u32() uint32 { return binary.BigEndian.Uint32(r.next(4)) }

// decryptESDK is an independent reading of the v1 format, used to check
// EncryptMessage field by field. The Node golden test checks it against the
// real AWS Encryption SDK.
func decryptESDK(t *testing.T, s *Service, msg []byte, ctx map[string]string) (keyARN string, plaintext []byte) {
	t.Helper()
	r := &esdkReader{t: t, b: msg}
	if v, typ, suite := r.u8(), r.u8(), r.u16(); v != 1 || typ != 0x80 || suite != 0x0178 {
		t.Fatalf("version/type/suite = %d/%x/%04x", v, typ, suite)
	}
	messageID := r.next(16)
	aad := r.next(r.u16())
	if !bytes.Equal(aad, SerializeEncryptionContext(ctx)) {
		t.Fatalf("header AAD = %x, want serialized context", aad)
	}
	if n := r.u16(); n != 1 {
		t.Fatalf("encrypted data key count = %d", n)
	}
	provider, info, edk := string(r.next(r.u16())), string(r.next(r.u16())), r.next(r.u16())
	if provider != "aws-kms" {
		t.Fatalf("provider ID = %q", provider)
	}
	if ct, reserved, ivLen, frameLen := r.u8(), r.next(4), r.u8(), r.u32(); ct != 2 || !bytes.Equal(reserved, []byte{0, 0, 0, 0}) || ivLen != 12 || frameLen != 4096 {
		t.Fatalf("content type/reserved/iv length/frame length = %d/%x/%d/%d", ct, reserved, ivLen, frameLen)
	}
	headerBody := msg[:r.off]

	dec, err := s.Decrypt(&DecryptInput{CiphertextBlob: edk, KeyId: info, EncryptionContext: ctx})
	if err != nil {
		t.Fatalf("decrypt data key: %v", err)
	}
	derived, err := hkdf.Key(sha256.New, dec.Plaintext, nil, string(append([]byte{0x01, 0x78}, messageID...)), 32)
	if err != nil {
		t.Fatal(err)
	}
	gcm, _ := newGCM(derived)
	headerIV, headerTag := r.next(12), r.next(16)
	if !bytes.Equal(headerIV, make([]byte, 12)) {
		t.Fatalf("header IV = %x, want zero", headerIV)
	}
	if _, err := gcm.Open(nil, headerIV, headerTag, headerBody); err != nil {
		t.Fatalf("header authentication: %v", err)
	}

	for want := uint32(1); ; want++ {
		final := false
		seq := r.u32()
		if seq == 0xFFFFFFFF {
			final = true
			seq = r.u32()
		}
		if seq != want {
			t.Fatalf("frame sequence = %d, want %d", seq, want)
		}
		iv := r.next(12)
		if !bytes.Equal(iv, frameIV(seq)) {
			t.Fatalf("frame %d IV = %x", seq, iv)
		}
		n, content := 4096, frameAAD
		if final {
			n, content = int(r.u32()), finalFrameAAD
		}
		ct := r.next(n + 16)
		pt, err := gcm.Open(nil, iv, ct, bodyAAD(messageID, content, seq, n))
		if err != nil {
			t.Fatalf("frame %d: %v", seq, err)
		}
		plaintext = append(plaintext, pt...)
		if final {
			break
		}
	}
	if r.off != len(msg) {
		t.Fatalf("%d trailing bytes", len(msg)-r.off)
	}
	return info, plaintext
}

func TestEncryptMessageFollowsESDKFormat(t *testing.T) {
	s := newTestService(t)
	m := createKey(t, s)
	ctx := map[string]string{"userpool-id": "eu-west-2_abc"}

	for _, size := range []int{0, 6, 4095, 4096, 4097, 10000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			plaintext := bytes.Repeat([]byte{'x'}, size)
			_, err := s.EncryptMessage("alias/missing", plaintext, ctx)
			wantCode(t, err, "NotFoundException")

			msg, err := s.EncryptMessage(m.KeyId, plaintext, ctx)
			if err != nil {
				t.Fatalf("EncryptMessage: %v", err)
			}
			keyARN, got := decryptESDK(t, s, msg, ctx)
			if keyARN != m.Arn {
				t.Fatalf("provider info = %q, want key ARN %q", keyARN, m.Arn)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("plaintext round trip failed for %d bytes", size)
			}
		})
	}
}
