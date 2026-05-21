package upload

import (
	"strings"
	"testing"

	"customer-service/backend/internal/config"
)

func TestS3PublicURLUsesCDNBase(t *testing.T) {
	got := publicURL(config.Config{
		S3PublicBaseURL: "https://cdn.example.com/assets/",
	}, "bucket", "uploads/20260513/a.png")
	if got != "https://cdn.example.com/assets/uploads/20260513/a.png" {
		t.Fatalf("unexpected public url: %s", got)
	}
}

func TestObjectKeyIncludesPrefixAndDate(t *testing.T) {
	got := objectKey(config.Config{S3KeyPrefix: "customer-service"}, ".png")
	if !strings.HasPrefix(got, "customer-service/") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("unexpected key: %s", got)
	}
}

func TestDetectUploadMimeTypeFallsBackToDeclaredType(t *testing.T) {
	got := detectUploadMimeType([]byte{0x01, 0x02, 0x03, 0x04}, "audio/webm;codecs=opus", "voice.bin")
	if got != "audio/webm" {
		t.Fatalf("unexpected mime type: %s", got)
	}
}

func TestDetectUploadMimeTypeFallsBackToFilename(t *testing.T) {
	got := detectUploadMimeType([]byte{0x01, 0x02, 0x03, 0x04}, "application/octet-stream", "voice.m4a")
	if got != "audio/mp4" {
		t.Fatalf("unexpected mime type from filename: %s", got)
	}
}
