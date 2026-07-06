package grpcclient

import (
	"context"
	"errors"
	"testing"
	"time"

	uploadv1 "github.com/meme-launchpad/app-rebuild/gen/upload/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ httpapi.Presigner = (*UploadService)(nil)

type fakeUploadClient struct {
	presignRequest *uploadv1.PresignImageRequest
	presignResult  *uploadv1.PresignImageResponse
	presignError   error
	confirmResult  *uploadv1.ConfirmUploadResponse
	confirmError   error
	token          string
}

func (f *fakeUploadClient) PresignImage(ctx context.Context, request *uploadv1.PresignImageRequest, _ ...grpc.CallOption) (*uploadv1.PresignImageResponse, error) {
	f.presignRequest = request
	f.token = firstMetadataValue(ctx, "authorization")
	return f.presignResult, f.presignError
}

func (f *fakeUploadClient) ConfirmUpload(ctx context.Context, _ *uploadv1.ConfirmUploadRequest, _ ...grpc.CallOption) (*uploadv1.ConfirmUploadResponse, error) {
	f.token = firstMetadataValue(ctx, "authorization")
	return f.confirmResult, f.confirmError
}

func TestUploadServicePresignsAndConvertsResponse(t *testing.T) {
	expires := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	client := &fakeUploadClient{presignResult: &uploadv1.PresignImageResponse{
		UploadUrl: "https://upload.example", PublicUrl: "https://cdn.example/image.webp",
		FileName: "image.webp", Key: "token-banner/56/image.webp", ExpiresAt: timestamppb.New(expires),
	}}
	service := &UploadService{client: client}
	ctx := auth.WithBearerToken(context.Background(), "jwt-value")

	result, err := service.Presign(ctx, "token-banner", "image/webp", 56)
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	if client.token != "Bearer jwt-value" || client.presignRequest.GetKind() != uploadv1.ImageKind_IMAGE_KIND_TOKEN_BANNER || client.presignRequest.GetChainId() != 56 {
		t.Fatalf("metadata = %q, request = %+v", client.token, client.presignRequest)
	}
	if result.Key != "token-banner/56/image.webp" || result.Expires != expires.Unix() {
		t.Fatalf("result = %+v", result)
	}
}

func TestUploadServiceConfirmsWithBearerToken(t *testing.T) {
	client := &fakeUploadClient{confirmResult: &uploadv1.ConfirmUploadResponse{Ok: true}}
	service := &UploadService{client: client}
	err := service.Confirm(auth.WithBearerToken(context.Background(), "jwt-value"))
	if err != nil || client.token != "Bearer jwt-value" {
		t.Fatalf("Confirm() error = %v, metadata = %q", err, client.token)
	}
}

func TestUploadServicePreservesRPCError(t *testing.T) {
	rpcError := status.Error(codes.Unavailable, "upload service unavailable")
	service := &UploadService{client: &fakeUploadClient{presignError: rpcError}}
	_, err := service.Presign(auth.WithBearerToken(context.Background(), "jwt-value"), "token-logo", "image/png", 97)
	if !errors.Is(err, rpcError) {
		t.Fatalf("Presign() error = %v, want wrapped RPC error", err)
	}
}
