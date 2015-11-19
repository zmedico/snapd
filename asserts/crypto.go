// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package asserts

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	_ "crypto/sha256" // be explicit about needing SHA256
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// TODO: eventually this should be the only non-test file using/importing directly from golang.org/x/crypto

func generatePrivateKey() (*packet.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return packet.NewRSAPrivateKey(time.Now(), priv), nil
}

var openpgpConfig = &packet.Config{
	DefaultHash: crypto.SHA256,
}

func signContent(content []byte, privKey *packet.PrivateKey) ([]byte, error) {
	sig := new(packet.Signature)
	sig.PubKeyAlgo = privKey.PubKeyAlgo
	sig.Hash = openpgpConfig.Hash()
	sig.CreationTime = time.Now()
	sig.IssuerKeyId = &privKey.KeyId

	h := openpgpConfig.Hash().New()
	h.Write(content)

	err := sig.Sign(h, privKey, openpgpConfig)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBufferString("openpgp ")
	enc := base64.NewEncoder(base64.StdEncoding, buf)
	err = sig.Serialize(enc)
	if err != nil {
		return nil, err
	}
	enc.Close()

	return buf.Bytes(), nil
}

func splitFormatAndDecode(formatAndBase64 []byte) (string, []byte, error) {
	parts := bytes.SplitN(formatAndBase64, []byte(" "), 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("expected format and base64 data separated by space")
	}
	buf := make([]byte, base64.StdEncoding.DecodedLen(len(parts[1])))
	n, err := base64.StdEncoding.Decode(buf, parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("could not decode base64 data: %v", err)
	}
	return string(parts[0]), buf[:n], nil
}

// PublicKeyPrim offers the operations of a public key crypto primitive.
type PublicKeyPrim interface {
	// Fingerprint returns the key fingerprint.
	Fingerprint() string
	// Verify verifies signature is valid for content using the key.
	Verify(content []byte, sig SignaturePrim) error
}

// TODO: have parse public key logic live here

// SignaturePrim carries a signature crypto primitive.
type SignaturePrim interface {
	// KeyID() returns a suffix of the signing key fingerprint
	KeyID() string
}

type signatureImpl struct {
	sig *packet.Signature
}

func (simpl signatureImpl) KeyID() string {
	return fmt.Sprintf("%016x", *simpl.sig.IssuerKeyId)
}

func verifyContentSignature(content []byte, sig SignaturePrim, pubKey *packet.PublicKey) error {
	sigImpl, ok := sig.(signatureImpl)
	if !ok {
		return fmt.Errorf("not an internally supported SignaturePrim: %T", sig)
	}

	h := openpgpConfig.Hash().New()
	h.Write(content)
	return pubKey.VerifySignature(h, sigImpl.sig)
}

func parseSignature(signature []byte) (SignaturePrim, error) {
	if len(signature) == 0 {
		return nil, fmt.Errorf("unexpected empty signature")
	}
	format, sigData, err := splitFormatAndDecode(signature)
	if err != nil {
		return nil, fmt.Errorf("signature: %v", err)
	}
	if format != "openpgp" {
		return nil, fmt.Errorf("unsupported signature format: %q", format)
	}
	pkt, err := packet.Read(bytes.NewReader(sigData))
	if err != nil {
		return nil, fmt.Errorf("could not parse signature data: %v", err)
	}
	sig, ok := pkt.(*packet.Signature)
	if !ok {
		return nil, fmt.Errorf("expected signature, got instead: %T", pkt)
	}
	if sig.IssuerKeyId == nil {
		return nil, fmt.Errorf("expected issuer keyid in signature")
	}
	return signatureImpl{sig}, nil
}

type trustedKey struct {
	pubKey *packet.PublicKey
	fp     string
}

func (tk *trustedKey) IsValidAt(time time.Time) bool {
	// XXX: naive for now until we know if we need trustedKey longer
	return true
}

func (tk *trustedKey) Fingerprint() string {
	return tk.fp
}

func (tk *trustedKey) Verify(content []byte, sig SignaturePrim) error {
	return verifyContentSignature(content, sig, tk.pubKey)
}

// TrustedKey returns a database useable trusted key out of a opengpg packet.PulicKey.
func TrustedKey(pubKey *packet.PublicKey) PublicKey {
	return &trustedKey{pubKey: pubKey, fp: hex.EncodeToString(pubKey.Fingerprint[:])}
}
