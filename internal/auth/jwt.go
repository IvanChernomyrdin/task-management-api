package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Manager struct {
	Secret []byte
	Issuer string
	TTL    time.Duration
}
type claims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var jwtEncoding = base64.RawURLEncoding

func (m Manager) Sign(id int64) (string, error) {
	header := jwtEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now().UTC()

	payload, err := json.Marshal(claims{
		Subject:   strconv.FormatInt(id, 10),
		Issuer:    m.Issuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.TTL).Unix(),
	})
	if err != nil {
		return "", err
	}

	body := header + "." + jwtEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.Secret)

	mac.Write([]byte(body))

	return body + "." + jwtEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (m Manager) Verify(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, errors.New("invalid JWT format")
	}

	sig, err := jwtEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, errors.New("invalid JWT signature")
	}

	mac := hmac.New(sha256.New, m.Secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))

	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, errors.New("invalid JWT signature")
	}

	headerBytes, err := jwtEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, errors.New("invalid JWT header")
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}

	if json.Unmarshal(headerBytes, &header) != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return 0, errors.New("invalid JWT header")
	}

	payload, err := jwtEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errors.New("invalid JWT payload")
	}

	var c claims
	if json.Unmarshal(payload, &c) != nil {
		return 0, errors.New("invalid JWT payload")
	}

	now := time.Now().Unix()
	if c.Issuer != m.Issuer ||
		c.IssuedAt <= 0 ||
		c.IssuedAt > now+60 ||
		c.ExpiresAt <= now ||
		c.ExpiresAt <= c.IssuedAt {
		return 0, errors.New("JWT expired or invalid")
	}

	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid JWT subject")
	}

	return id, nil
}
