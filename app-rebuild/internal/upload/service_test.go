package upload

import (
	"strings"
	"testing"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/config"
)

func TestPresignBuildsCOSUploadAndPublicURLs(t *testing.T) {
	service, err := New(config.COSConfig{SecretID: "id", SecretKey: "key", Bucket: "bucket", Region: "ap-guangzhou", Domain: "https://cdn.example"})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	result, err := service.Presign("token-logo", "image/webp", 97)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(result.Key, "token-logo/97/") || !strings.HasSuffix(result.FileName, ".webp") {
		t.Fatalf("key=%s fileName=%s", result.Key, result.FileName)
	}
	if !strings.HasPrefix(result.UploadURL, "https://bucket.cos.ap-guangzhou.myqcloud.com/token-logo/97/") {
		t.Fatalf("upload URL = %s", result.UploadURL)
	}
	if !strings.Contains(result.UploadURL, "q-ak=id") || !strings.Contains(result.UploadURL, "q-signature=") {
		t.Fatalf("upload URL missing COS signature params: %s", result.UploadURL)
	}
	if result.PublicURL != "https://cdn.example/"+result.Key {
		t.Fatalf("public URL = %s", result.PublicURL)
	}
	if result.Expires != 1_700_003_600 {
		t.Fatalf("expires = %d", result.Expires)
	}
}
