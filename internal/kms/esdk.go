package kms

import (
	"bytes"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
)

// AWS Encryption SDK message format, version 1.0, as specified in
// aws-encryption-sdk-specification/data-format. Tarn writes one algorithm
// suite, ALG_AES_256_GCM_IV12_TAG16_HKDF_SHA256 (0x0178): no key commitment
// and no signature. Decrypters accept it under the
// REQUIRE_ENCRYPT_ALLOW_DECRYPT commitment policy, which is the policy
// Cognito's custom sender documentation uses.
const (
	esdkVersion     = 0x01
	esdkTypeAEAD    = 0x80
	esdkSuiteID     = 0x0178
	esdkContentType = 0x02 // framed
	esdkFrameLength = 4096
	esdkProviderID  = "aws-kms"

	frameAAD      = "AWSKMSEncryptionClient Frame"
	finalFrameAAD = "AWSKMSEncryptionClient Final Frame"
	finalFrameEnd = 0xFFFFFFFF
)

// EncryptMessage encrypts plaintext as an AWS Encryption SDK message whose
// data key is protected by keyID, as a KmsKeyringNode with that key as its
// generator would. The encryption context is bound to both the message and
// the encrypted data key.
func (s *Service) EncryptMessage(keyID string, plaintext []byte, ctx map[string]string) ([]byte, error) {
	s.mu.Lock()
	k, err := s.usableLocked(keyID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	edk, err := s.seal(k, dataKey, ctx)
	keyARN := s.keyARN(k.ID)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	messageID := make([]byte, 16)
	if _, err := rand.Read(messageID); err != nil {
		return nil, err
	}

	var header bytes.Buffer
	header.WriteByte(esdkVersion)
	header.WriteByte(esdkTypeAEAD)
	header.Write(be16(esdkSuiteID))
	header.Write(messageID)
	aad := SerializeEncryptionContext(ctx)
	header.Write(be16(len(aad)))
	header.Write(aad)
	header.Write(be16(1)) // encrypted data key count
	writeBytes16(&header, []byte(esdkProviderID))
	writeBytes16(&header, []byte(keyARN))
	writeBytes16(&header, edk)
	header.WriteByte(esdkContentType)
	header.Write([]byte{0, 0, 0, 0}) // reserved
	header.WriteByte(nonceLen)
	header.Write(be32(esdkFrameLength))

	// HKDF with a zero salt and info = suite ID || message ID.
	info := append(be16(esdkSuiteID), messageID...)
	derived, err := hkdf.Key(sha256.New, dataKey, make([]byte, sha256.Size), string(info), 32)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(derived)
	if err != nil {
		return nil, err
	}

	// Header authentication: the tag over the header body, with a zero IV.
	msg := bytes.NewBuffer(header.Bytes())
	zeroIV := make([]byte, nonceLen)
	msg.Write(zeroIV)
	msg.Write(gcm.Seal(nil, zeroIV, nil, header.Bytes()))

	// Framed body: full regular frames, then a final frame holding the rest.
	// A plaintext that is an exact multiple of the frame length ends with a
	// full final frame.
	seq := uint32(1)
	rest := plaintext
	for len(rest) > esdkFrameLength {
		chunk := rest[:esdkFrameLength]
		iv := frameIV(seq)
		msg.Write(be32(int(seq)))
		msg.Write(iv)
		msg.Write(gcm.Seal(nil, iv, chunk, bodyAAD(messageID, frameAAD, seq, len(chunk))))
		rest = rest[esdkFrameLength:]
		seq++
	}
	iv := frameIV(seq)
	msg.Write(binary.BigEndian.AppendUint32(nil, finalFrameEnd))
	msg.Write(be32(int(seq)))
	msg.Write(iv)
	msg.Write(be32(len(rest)))
	msg.Write(gcm.Seal(nil, iv, rest, bodyAAD(messageID, finalFrameAAD, seq, len(rest))))
	return msg.Bytes(), nil
}

// frameIV is the frame's sequence number, big-endian, left-padded with zeros
// to the IV length.
func frameIV(seq uint32) []byte {
	iv := make([]byte, nonceLen)
	binary.BigEndian.PutUint32(iv[nonceLen-4:], seq)
	return iv
}

// bodyAAD is message ID || body AAD content || sequence number || content
// length (8 bytes).
func bodyAAD(messageID []byte, content string, seq uint32, length int) []byte {
	aad := append(bytes.Clone(messageID), content...)
	aad = binary.BigEndian.AppendUint32(aad, seq)
	return binary.BigEndian.AppendUint64(aad, uint64(length))
}

func be16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }

func be32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func writeBytes16(b *bytes.Buffer, v []byte) {
	b.Write(be16(len(v)))
	b.Write(v)
}
