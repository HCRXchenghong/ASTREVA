package upload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"customer-service/backend/internal/config"
	"customer-service/backend/internal/store"
)

type Result struct {
	URL      string `json:"url"`
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
}

func SaveHTTP(w http.ResponseWriter, r *http.Request, cfg config.Config) (Result, error) {
	maxBytes := cfg.UploadMaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		return Result{}, store.ErrInvalidInput
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return Result{}, store.ErrInvalidInput
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(content)) > maxBytes {
		return Result{}, store.ErrInvalidInput
	}
	mimeType := detectUploadMimeType(content, header.Header.Get("Content-Type"), header.Filename)
	ext := extension(mimeType)
	if ext == "" {
		return Result{}, store.ErrInvalidInput
	}
	key := objectKey(cfg, ext)
	if strings.EqualFold(strings.TrimSpace(cfg.UploadDriver), "s3") {
		return saveS3(r.Context(), cfg, key, mimeType, content)
	}
	return saveLocal(cfg, key, mimeType, content)
}

func LocalFileHandler(cfg config.Config) http.Handler {
	files := http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

func saveLocal(cfg config.Config, key string, mimeType string, content []byte) (Result, error) {
	key = strings.TrimPrefix(key, "uploads/")
	fullPath := filepath.Join(cfg.UploadDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return Result{}, err
	}
	urlPath := "/uploads/" + key
	publicBase := strings.TrimRight(strings.TrimSpace(cfg.UploadPublicBaseURL), "/")
	if publicBase != "" {
		urlPath = publicBase + urlPath
	}
	return Result{URL: urlPath, Path: key, MimeType: mimeType, Size: len(content)}, nil
}

func saveS3(ctx context.Context, cfg config.Config, key string, mimeType string, content []byte) (Result, error) {
	bucket := strings.TrimSpace(cfg.S3Bucket)
	if bucket == "" {
		return Result{}, store.ErrInvalidInput
	}
	awsOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(nonEmpty(cfg.S3Region, "us-east-1")),
	}
	if cfg.S3AccessKeyID != "" || cfg.S3SecretAccessKey != "" {
		awsOptions = append(awsOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKeyID,
			cfg.S3SecretAccessKey,
			cfg.S3SessionToken,
		)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsOptions...)
	if err != nil {
		return Result{}, err
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.S3ForcePathStyle
		if strings.TrimSpace(cfg.S3Endpoint) != "" {
			options.BaseEndpoint = aws.String(strings.TrimRight(strings.TrimSpace(cfg.S3Endpoint), "/"))
		}
	})
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(content),
		ContentType:  aws.String(mimeType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{URL: publicURL(cfg, bucket, key), Path: key, MimeType: mimeType, Size: len(content)}, nil
}

func objectKey(cfg config.Config, ext string) string {
	prefix := strings.Trim(strings.TrimSpace(cfg.S3KeyPrefix), "/")
	if prefix == "" {
		prefix = "uploads"
	}
	return prefix + "/" + time.Now().UTC().Format("20060102") + "/" + randomName() + ext
}

func publicURL(cfg config.Config, bucket string, key string) string {
	publicBase := strings.TrimRight(strings.TrimSpace(cfg.S3PublicBaseURL), "/")
	if publicBase == "" {
		publicBase = strings.TrimRight(strings.TrimSpace(cfg.UploadPublicBaseURL), "/")
	}
	if publicBase != "" {
		return publicBase + "/" + key
	}
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.S3Endpoint), "/")
	if endpoint != "" {
		if cfg.S3ForcePathStyle {
			return endpoint + "/" + bucket + "/" + key
		}
		endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
		return "https://" + bucket + "." + endpoint + "/" + key
	}
	return "https://" + bucket + ".s3." + nonEmpty(cfg.S3Region, "us-east-1") + ".amazonaws.com/" + key
}

func extension(mimeType string) string {
	mimeType = normalizeMimeType(mimeType)
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/x-m4a", "audio/mp4a-latm", "video/mp4":
		return ".m4a"
	case "audio/ogg", "application/ogg":
		return ".ogg"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/aac":
		return ".aac"
	default:
		return ""
	}
}

func detectUploadMimeType(content []byte, declaredType, filename string) string {
	detected := normalizeMimeType(http.DetectContentType(content))
	if extension(detected) != "" {
		return detected
	}
	declaredType = normalizeMimeType(declaredType)
	if extension(declaredType) != "" {
		return declaredType
	}
	return mimeTypeFromFilename(filename)
}

func normalizeMimeType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ";")[0])
}

func mimeTypeFromFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filename))) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".webm":
		return "audio/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".mp4":
		return "video/mp4"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	default:
		return ""
	}
}

func randomName() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
