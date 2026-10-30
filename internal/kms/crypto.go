package kms

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"sort"
)

// Ciphertext blob layout, Tarn's own:
//
//	version (1) | key ID (36, UUID text) | nonce (12) | AES-256-GCM ciphertext and tag
//
// The GCM additional data is the version and key ID followed by the
// encryption context, serialized as the AWS Encryption SDK does, so a blob
// only decrypts under its key and its exact context.
const (
	blobVersion = 0x01
	keyIDLen    = 36
	nonceLen    = 12
	tagLen      = 16
	blobHeader  = 1 + keyIDLen + nonceLen

	maxPlaintext = 4096
)

// SerializeEncryptionContext encodes ctx as the AWS Encryption SDK does: a
// pair count, then length-prefixed key-value pairs sorted by key bytes. An
// empty context encodes as no bytes.
func SerializeEncryptionContext(ctx map[string]string) []byte {
	if len(ctx) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys) // byte-wise, which is UTF-8 binary order
	var b bytes.Buffer
	b.Write(binary.BigEndian.AppendUint16(nil, uint16(len(keys))))
	for _, k := range keys {
		writeField(&b, k)
		writeField(&b, ctx[k])
	}
	return b.Bytes()
}

func writeField(b *bytes.Buffer, v string) {
	b.Write(binary.BigEndian.AppendUint16(nil, uint16(len(v))))
	b.WriteString(v)
}

func blobAAD(keyID string, ctx map[string]string) []byte {
	aad := append([]byte{blobVersion}, keyID...)
	return append(aad, SerializeEncryptionContext(ctx)...)
}

func newGCM(material []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(material)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Service) seal(k *key, plaintext []byte, ctx map[string]string) ([]byte, error) {
	gcm, err := newGCM(k.material)
	if err != nil {
		return nil, err
	}
	blob := make([]byte, blobHeader, blobHeader+len(plaintext)+tagLen)
	blob[0] = blobVersion
	copy(blob[1:], k.ID)
	nonce := blob[1+keyIDLen : blobHeader]
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(blob, nonce, plaintext, blobAAD(k.ID, ctx)), nil
}

// openLocked decrypts a blob, returning the key that made it. keyID, when
// set, must name that same key. The key is returned with the error too once
// the blob has identified it, so failures can be counted against it.
func (s *Service) openLocked(blob []byte, keyID string, ctx map[string]string) (*key, []byte, error) {
	if len(blob) < blobHeader+tagLen || blob[0] != blobVersion {
		return nil, nil, errInvalidCiphertext
	}
	k := s.keys[string(blob[1:1+keyIDLen])]
	if k == nil || s.expiredLocked(k) {
		return nil, nil, errInvalidCiphertext
	}
	if keyID != "" {
		named, err := s.resolveLocked(keyID)
		if err != nil {
			return nil, nil, err
		}
		if named.ID != k.ID {
			return k, nil, errIncorrectKey
		}
	}
	if err := s.checkUsable(k); err != nil {
		return k, nil, err
	}
	gcm, err := newGCM(k.material)
	if err != nil {
		return k, nil, err
	}
	plaintext, err := gcm.Open(nil, blob[1+keyIDLen:blobHeader], blob[blobHeader:], blobAAD(k.ID, ctx))
	if err != nil {
		return k, nil, errInvalidCiphertext
	}
	return k, plaintext, nil
}

func checkAlgorithm(alg string) error {
	if alg != "" && alg != symmetricDefault {
		return newError("InvalidKeyUsageException", "Algorithm %s is incompatible with a SYMMETRIC_DEFAULT key.", alg)
	}
	return nil
}

// Encrypt encrypts up to 4 KiB of plaintext under a key.
func (s *Service) Encrypt(in *EncryptInput) (*EncryptOutput, error) {
	if err := checkAlgorithm(in.EncryptionAlgorithm); err != nil {
		return nil, err
	}
	if len(in.Plaintext) == 0 || len(in.Plaintext) > maxPlaintext {
		return nil, validation("1 validation error detected: Value at 'plaintext' failed to satisfy constraint: Member must have length between 1 and %d", maxPlaintext)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.usableLocked(in.KeyId)
	if err != nil {
		s.recordLocked(k, "encrypt", err)
		return nil, err
	}
	blob, err := s.seal(k, in.Plaintext, in.EncryptionContext)
	s.recordLocked(k, "encrypt", err)
	if err != nil {
		return nil, err
	}
	return &EncryptOutput{CiphertextBlob: blob, KeyId: s.keyARN(k.ID), EncryptionAlgorithm: symmetricDefault}, nil
}

// Decrypt decrypts a blob made by Encrypt, GenerateDataKey or ReEncrypt. The
// blob names its key, so KeyId is optional.
func (s *Service) Decrypt(in *DecryptInput) (*DecryptOutput, error) {
	if err := checkAlgorithm(in.EncryptionAlgorithm); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, plaintext, err := s.openLocked(in.CiphertextBlob, in.KeyId, in.EncryptionContext)
	s.recordLocked(k, "decrypt", err)
	if err != nil {
		return nil, err
	}
	return &DecryptOutput{KeyId: s.keyARN(k.ID), Plaintext: plaintext, EncryptionAlgorithm: symmetricDefault}, nil
}

// ReEncrypt decrypts a blob and encrypts the plaintext under another key or
// encryption context, without returning the plaintext.
func (s *Service) ReEncrypt(in *ReEncryptInput) (*ReEncryptOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, plaintext, err := s.openLocked(in.CiphertextBlob, in.SourceKeyId, in.SourceEncryptionContext)
	if err != nil {
		s.recordLocked(src, "reEncrypt", err)
		return nil, err
	}
	dst, err := s.usableLocked(in.DestinationKeyId)
	if err != nil {
		s.recordLocked(dst, "reEncrypt", err)
		return nil, err
	}
	blob, err := s.seal(dst, plaintext, in.DestinationEncryptionContext)
	s.recordLocked(src, "reEncrypt", err)
	if src != dst {
		s.recordLocked(dst, "reEncrypt", err)
	}
	if err != nil {
		return nil, err
	}
	return &ReEncryptOutput{
		CiphertextBlob:                 blob,
		SourceKeyId:                    s.keyARN(src.ID),
		KeyId:                          s.keyARN(dst.ID),
		SourceEncryptionAlgorithm:      symmetricDefault,
		DestinationEncryptionAlgorithm: symmetricDefault,
	}, nil
}

// GenerateDataKey returns a random data key in plaintext and encrypted under
// the key.
func (s *Service) GenerateDataKey(in *GenerateDataKeyInput) (*GenerateDataKeyOutput, error) {
	size, err := dataKeySize(in.KeySpec, in.NumberOfBytes)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.usableLocked(in.KeyId)
	if err != nil {
		s.recordLocked(k, "dataKey", err)
		return nil, err
	}
	plaintext := make([]byte, size)
	if _, err := rand.Read(plaintext); err != nil {
		return nil, err
	}
	blob, err := s.seal(k, plaintext, in.EncryptionContext)
	s.recordLocked(k, "dataKey", err)
	if err != nil {
		return nil, err
	}
	return &GenerateDataKeyOutput{CiphertextBlob: blob, Plaintext: plaintext, KeyId: s.keyARN(k.ID)}, nil
}

// GenerateDataKeyWithoutPlaintext is GenerateDataKey returning only the
// encrypted copy.
func (s *Service) GenerateDataKeyWithoutPlaintext(in *GenerateDataKeyInput) (*GenerateDataKeyOutput, error) {
	out, err := s.GenerateDataKey(in)
	if err != nil {
		return nil, err
	}
	out.Plaintext = nil
	return out, nil
}

func dataKeySize(spec string, n int) (int, error) {
	switch {
	case spec != "" && n != 0:
		return 0, validation("Please specify either number of bytes or key spec.")
	case spec == "AES_256":
		return 32, nil
	case spec == "AES_128":
		return 16, nil
	case spec != "":
		return 0, validation("1 validation error detected: Value '%s' at 'keySpec' failed to satisfy constraint: Member must satisfy enum value set: [AES_256, AES_128]", spec)
	case n >= 1 && n <= 1024:
		return n, nil
	case n != 0:
		return 0, validation("1 validation error detected: Value '%d' at 'numberOfBytes' failed to satisfy constraint: Member must have value between 1 and 1024", n)
	}
	return 0, validation("Please specify either number of bytes or key spec.")
}

// GenerateRandom returns 1 to 1024 random bytes.
func (s *Service) GenerateRandom(in *GenerateRandomInput) (*GenerateRandomOutput, error) {
	if in.NumberOfBytes < 1 || in.NumberOfBytes > 1024 {
		return nil, validation("1 validation error detected: Value '%d' at 'numberOfBytes' failed to satisfy constraint: Member must have value between 1 and 1024", in.NumberOfBytes)
	}
	out := make([]byte, in.NumberOfBytes)
	if _, err := rand.Read(out); err != nil {
		return nil, err
	}
	return &GenerateRandomOutput{Plaintext: out}, nil
}
