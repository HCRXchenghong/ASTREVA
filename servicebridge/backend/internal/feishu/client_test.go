package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func testClient() *Client {
	return NewClient(Config{
		Enabled:           true,
		AppID:             "cli_test",
		AppSecret:         "secret",
		VerificationToken: "verify-token",
		EncryptKey:        "encrypt-key",
		DefaultChatID:     "oc_test",
	}, nil)
}

func TestParseCallbackChallenge(t *testing.T) {
	callback, err := testClient().ParseCallback([]byte(`{
		"challenge": "challenge_value",
		"token": "verify-token",
		"type": "url_verification"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if callback.Challenge != "challenge_value" {
		t.Fatalf("unexpected challenge: %q", callback.Challenge)
	}
}

func TestParseCallbackMessage(t *testing.T) {
	callback, err := testClient().ParseCallback([]byte(`{
		"schema": "2.0",
		"header": {
			"event_id": "evt_1",
			"event_type": "im.message.receive_v1",
			"token": "verify-token"
		},
		"event": {
			"sender": {
				"sender_type": "user",
				"sender_id": {"open_id": "ou_1"}
			},
			"message": {
				"message_id": "om_1",
				"root_id": "om_root",
				"parent_id": "om_parent",
				"chat_id": "oc_1",
				"message_type": "text",
				"content": "{\"text\":\"<at user_id=\\\"bot\\\">bot</at> 已收到 #conv_abcd\"}"
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if callback.Message == nil {
		t.Fatal("expected message callback")
	}
	if callback.Message.EventID != "evt_1" || callback.Message.ParentID != "om_parent" {
		t.Fatalf("unexpected callback: %#v", callback.Message)
	}
	if callback.Message.Text != "已收到 #conv_abcd" {
		t.Fatalf("unexpected cleaned text: %q", callback.Message.Text)
	}
}

func TestParseCallbackRejectsInvalidToken(t *testing.T) {
	if _, err := testClient().ParseCallback([]byte(`{
		"challenge": "challenge_value",
		"token": "bad-token",
		"type": "url_verification"
	}`)); err == nil {
		t.Fatal("expected invalid token error")
	}
}

func TestParseCallbackEncryptedMessage(t *testing.T) {
	plain := []byte(`{
		"schema": "2.0",
		"header": {
			"event_id": "evt_encrypted",
			"event_type": "im.message.receive_v1",
			"token": "verify-token"
		},
		"event": {
			"sender": {
				"sender_type": "user",
				"sender_id": {"open_id": "ou_1"}
			},
			"message": {
				"message_id": "om_2",
				"chat_id": "oc_1",
				"message_type": "text",
				"content": "{\"text\":\"加密消息 #conv_abcd\"}"
			}
		}
	}`)
	encrypted := encryptForTest(t, "encrypt-key", plain)
	callback, err := testClient().ParseCallback([]byte(`{"encrypt":"` + encrypted + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if callback.Message == nil || callback.Message.EventID != "evt_encrypted" {
		t.Fatalf("unexpected encrypted callback: %#v", callback.Message)
	}
}

func encryptForTest(t *testing.T, encryptKey string, plain []byte) string {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), byte(padding))
	for len(padded)%aes.BlockSize != 0 {
		padded = append(padded, byte(padding))
	}
	iv := []byte("1234567890abcdef")
	out := make([]byte, aes.BlockSize+len(padded))
	copy(out, iv)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[aes.BlockSize:], padded)
	return base64.StdEncoding.EncodeToString(out)
}
