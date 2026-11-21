// Package credentialbox seals account credentials before database writes.
package credentialbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrUnconfigured = errors.New("ключ хранилища учётных данных не настроен")

type Box struct {
	active string
	keys   map[string]cipher.AEAD
}

func New(version, key, previousVersion, previousKey string) (*Box, error) {
	b := &Box{active: version, keys: make(map[string]cipher.AEAD)}
	if key == "" && previousKey == "" {
		return b, nil
	}
	if key == "" || version == "" || (previousVersion == "") != (previousKey == "") || (previousVersion != "" && previousVersion == version) {
		return nil, errors.New("неверная конфигурация версий ключей учётных данных")
	}
	for ver, encoded := range map[string]string{version: key, previousVersion: previousKey} {
		if encoded == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("ключ учётных данных должен содержать 32 байта в Base64")
		}
		block, err := aes.NewCipher(decoded)
		if err != nil {
			return nil, err
		}
		b.keys[ver], err = cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (b *Box) Version() string { return b.active }

func (b *Box) Seal(clear, aad []byte) ([]byte, error) {
	a := b.keys[b.active]
	if a == nil {
		return nil, ErrUnconfigured
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, clear, aad), nil
}

func (b *Box) Open(version string, sealed, aad []byte) ([]byte, error) {
	a := b.keys[version]
	if a == nil {
		return nil, fmt.Errorf("ключ для версии %q недоступен", version)
	}
	size := a.NonceSize()
	if len(sealed) < size+a.Overhead() {
		return nil, errors.New("повреждён зашифрованный пакет учётных данных")
	}
	clear, err := a.Open(nil, sealed[:size], sealed[size:], aad)
	if err != nil {
		return nil, errors.New("не удалось проверить целостность учётных данных")
	}
	return clear, nil
}
