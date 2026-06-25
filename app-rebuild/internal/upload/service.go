package upload

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/config"
)

type Service struct {
	secretID  string
	secretKey string
	bucket    string
	region    string
	domain    string
	now       func() time.Time
}

type PresignResult struct {
	UploadURL string `json:"uploadUrl"`
	PublicURL string `json:"publicUrl"`
	FileName  string `json:"fileName"`
	Key       string `json:"key"`
	Expires   int64  `json:"expiresAt"`
}

func New(cfg config.COSConfig) (*Service, error) {
	if cfg.SecretID == "" || cfg.SecretKey == "" || cfg.Bucket == "" || cfg.Region == "" {
		return nil, fmt.Errorf("COS_SECRET_ID, COS_SECRET_KEY, COS_BUCKET, and COS_REGION are required")
	}
	return &Service{secretID: cfg.SecretID, secretKey: cfg.SecretKey, bucket: cfg.Bucket, region: cfg.Region, domain: cfg.Domain, now: time.Now}, nil
}

func (s *Service) Presign(folder, mimeType string, chainID int) (PresignResult, error) {
	if folder == "" {
		return PresignResult{}, fmt.Errorf("folder is required")
	}
	if chainID < 1 {
		return PresignResult{}, fmt.Errorf("chainId must be positive")
	}
	fileName, err := randomFileName(mimeType)
	if err != nil {
		return PresignResult{}, err
	}
	key := fmt.Sprintf("%s/%d/%s", folder, chainID, fileName)
	expiresAt := s.now().Add(time.Hour)
	uploadURL := s.presignedPutURL(key, expiresAt)
	return PresignResult{
		UploadURL: uploadURL,
		PublicURL: s.publicURL(key),
		FileName:  fileName,
		Key:       key,
		Expires:   expiresAt.Unix(),
	}, nil
}

func (s *Service) presignedPutURL(key string, expiresAt time.Time) string {
	host := fmt.Sprintf("%s.cos.%s.myqcloud.com", s.bucket, s.region)
	path := "/" + key
	start := s.now().Unix()
	end := expiresAt.Unix()
	keyTime := fmt.Sprintf("%d;%d", start, end)
	signKey := hmacSHA1(s.secretKey, keyTime)
	formatString := fmt.Sprintf("put\n%s\n\n\n", path)
	stringToSign := fmt.Sprintf("sha1\n%s\n%s\n", keyTime, sha1Hex(formatString))
	signature := hmacSHA1(signKey, stringToSign)

	query := url.Values{}
	query.Set("q-sign-algorithm", "sha1")
	query.Set("q-ak", s.secretID)
	query.Set("q-sign-time", keyTime)
	query.Set("q-key-time", keyTime)
	query.Set("q-header-list", "")
	query.Set("q-url-param-list", "")
	query.Set("q-signature", signature)
	return fmt.Sprintf("https://%s%s?%s", host, path, query.Encode())
}

func (s *Service) publicURL(key string) string {
	if s.domain != "" {
		return strings.TrimSuffix(s.domain, "/") + "/" + key
	}
	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", s.bucket, s.region, key)
}

func randomFileName(mimeType string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate file name: %w", err)
	}
	return hex.EncodeToString(bytes) + extension(mimeType), nil
}

func extension(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "jpg", "jpeg", "image/jpeg":
		return ".jpg"
	case "gif", "image/gif":
		return ".gif"
	case "webp", "image/webp":
		return ".webp"
	case "svg", "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

func hmacSHA1(key, data string) string {
	mac := hmac.New(sha1.New, []byte(key))
	_, _ = mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha1Hex(data string) string {
	hash := sha1.New()
	_, _ = hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}
