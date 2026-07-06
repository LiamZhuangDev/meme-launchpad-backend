package grpcapi

import (
	"context"
	"testing"

	uploadv1 "github.com/meme-launchpad/app-rebuild/gen/upload/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeUploadPresigner struct {
	folder   string
	mimeType string
	chainID  int
}

func (f *fakeUploadPresigner) Presign(_ context.Context, folder, mimeType string, chainID int) (upload.PresignResult, error) {
	f.folder, f.mimeType, f.chainID = folder, mimeType, chainID
	return upload.PresignResult{
		UploadURL: "https://upload.example", PublicURL: "https://cdn.example/image.webp",
		FileName: "image.webp", Key: folder + "/image.webp", Expires: 1_700_000_000,
	}, nil
}

func (f *fakeUploadPresigner) Confirm(context.Context) error { return nil }

func TestUploadPresignMapsImageKindAndRequiresAuthentication(t *testing.T) {
	const secret = "test-secret"
	authenticator := auth.NewJWTVerifier(secret)
	uploads := &fakeUploadPresigner{}
	connection := testConnection(t, Dependencies{UploadAuth: authenticator, Uploads: uploads})
	client := uploadv1.NewUploadServiceClient(connection)
	chainID := int32(56)
	request := &uploadv1.PresignImageRequest{
		Kind: uploadv1.ImageKind_IMAGE_KIND_TOKEN_BANNER, MimeType: "image/webp", ChainId: &chainID,
	}

	_, err := client.PresignImage(context.Background(), request)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated code = %s", status.Code(err))
	}
	ctx := authenticatedContext(t, secret, "0x3333333333333333333333333333333333333333", 7)
	response, err := client.PresignImage(ctx, request)
	if err != nil {
		t.Fatalf("PresignImage() error = %v", err)
	}
	if uploads.folder != "token-banner" || uploads.mimeType != "image/webp" || uploads.chainID != 56 {
		t.Fatalf("presign arguments = %s %s %d", uploads.folder, uploads.mimeType, uploads.chainID)
	}
	if response.UploadUrl != "https://upload.example" || response.ExpiresAt == nil {
		t.Fatalf("response = %+v", response)
	}
}

func TestUploadConfirmIsAuthenticatedPlaceholder(t *testing.T) {
	const secret = "test-secret"
	authenticator := auth.NewJWTVerifier(secret)
	connection := testConnection(t, Dependencies{UploadAuth: authenticator, Uploads: &fakeUploadPresigner{}})
	client := uploadv1.NewUploadServiceClient(connection)
	ctx := authenticatedContext(t, secret, "0x3333333333333333333333333333333333333333", 7)

	response, err := client.ConfirmUpload(ctx, &uploadv1.ConfirmUploadRequest{})
	if err != nil || !response.Ok {
		t.Fatalf("ConfirmUpload() = %+v, %v", response, err)
	}
}
