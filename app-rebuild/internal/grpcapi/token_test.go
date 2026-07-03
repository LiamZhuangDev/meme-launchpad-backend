package grpcapi

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeTokenReader struct {
	items       []repository.Token
	limit       int
	offset      int
	findAddress string
	findError   error
}

func (f *fakeTokenReader) List(_ context.Context, limit, offset int) ([]repository.Token, error) {
	f.limit, f.offset = limit, offset
	return f.items, nil
}

func (f *fakeTokenReader) FindByAddress(_ context.Context, address string) (repository.Token, error) {
	f.findAddress = address
	if f.findError != nil {
		return repository.Token{}, f.findError
	}
	return f.items[0], nil
}

func TestTokenServiceListUsesRESTEquivalentPagination(t *testing.T) {
	description := "community token"
	reader := &fakeTokenReader{items: []repository.Token{{
		ID: 7, Name: "Meme", Symbol: "MEME", Description: &description,
		ContractAddress: "0x1111111111111111111111111111111111111111",
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
	}}}
	client := tokenClient(t, reader)
	page, pageSize := int32(2), int32(5)

	response, err := client.ListTokens(context.Background(), &tokenv1.ListTokensRequest{Page: &page, PageSize: &pageSize})
	if err != nil {
		t.Fatalf("ListTokens() error = %v", err)
	}
	if reader.limit != 5 || reader.offset != 5 {
		t.Fatalf("repository pagination = limit %d offset %d, want 5 and 5", reader.limit, reader.offset)
	}
	if response.Page != 2 || response.PageSize != 5 || len(response.Items) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].Symbol != "MEME" || response.Items[0].GetDescription() != description {
		t.Fatalf("token = %+v", response.Items[0])
	}
}

func TestTokenServiceGetUsesContractAddress(t *testing.T) {
	reader := &fakeTokenReader{items: []repository.Token{{ID: 7, Symbol: "MEME"}}}
	client := tokenClient(t, reader)
	address := "0x1111111111111111111111111111111111111111"

	response, err := client.GetToken(context.Background(), &tokenv1.GetTokenRequest{ContractAddress: address})
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	if reader.findAddress != address || response.Token.Symbol != "MEME" {
		t.Fatalf("address = %q, response = %+v", reader.findAddress, response)
	}
}

func TestTokenServiceValidatesRequestsAndMapsNotFound(t *testing.T) {
	reader := &fakeTokenReader{items: []repository.Token{{}}, findError: pgx.ErrNoRows}
	client := tokenClient(t, reader)
	zero := int32(0)

	_, err := client.ListTokens(context.Background(), &tokenv1.ListTokensRequest{Page: &zero})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListTokens() code = %s, want InvalidArgument", status.Code(err))
	}
	_, err = client.GetToken(context.Background(), &tokenv1.GetTokenRequest{ContractAddress: "0xmissing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetToken() code = %s, want NotFound", status.Code(err))
	}
}

func tokenClient(t *testing.T, tokens TokenReader) tokenv1.TokenServiceClient {
	t.Helper()
	return tokenv1.NewTokenServiceClient(testConnection(t, Dependencies{Tokens: tokens}))
}
