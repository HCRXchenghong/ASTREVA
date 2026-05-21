package store

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	passwordPrefixBcrypt  = "bcrypt:"
	passwordPrefixDevText = "dev_plain:"
)

func hashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return passwordPrefixBcrypt + string(hash), nil
}

func verifyPassword(stored, password string) bool {
	stored = strings.TrimSpace(stored)
	password = strings.TrimSpace(password)
	if stored == "" || password == "" {
		return false
	}
	if strings.HasPrefix(stored, passwordPrefixDevText) {
		return strings.TrimPrefix(stored, passwordPrefixDevText) == password
	}
	if strings.HasPrefix(stored, passwordPrefixBcrypt) {
		return bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(stored, passwordPrefixBcrypt)), []byte(password)) == nil
	}
	return stored == password
}

func validateNewPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 10 {
		return ErrInvalidInput
	}
	if password == "123456" || strings.EqualFold(password, "password") {
		return ErrInvalidInput
	}
	return nil
}
