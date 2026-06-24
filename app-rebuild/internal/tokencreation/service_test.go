package tokencreation

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type memoryStore struct {
	request repository.TokenCreationRequest
}

func (s *memoryStore) Create(_ context.Context, request repository.TokenCreationRequest) error {
	s.request = request
	return nil
}

func TestCreateBuildsRecoverableContractSignatureAndPersistsIntent(t *testing.T) {
	key, err := ethcrypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{}
	core := common.HexToAddress("0x1111111111111111111111111111111111111111")
	service, err := New(Config{ChainID: 97, Core: core, Factory: common.HexToAddress("0x2222222222222222222222222222222222222222"), TokenCreationBytecode: []byte{1, 2, 3}, Signer: key}, store)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Create(context.Background(), Request{Name: "Meme", Symbol: "MEME", Creator: common.HexToAddress("0x3333333333333333333333333333333333333333"), InitialBuyPercentage: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if store.request.RequestID != result.RequestID || store.request.Data != result.Data {
		t.Fatalf("stored request = %+v, result = %+v", store.request, result)
	}
	data, _ := hex.DecodeString(result.Data[2:])
	signature, _ := hex.DecodeString(result.Signature[2:])
	if signature[64] != 27 && signature[64] != 28 {
		t.Fatalf("signature v = %d, want 27 or 28", signature[64])
	}
	signature[64] -= 27
	hash := ethcrypto.Keccak256Hash(data, uint256Bytes(97), core.Bytes())
	publicKey, err := ethcrypto.SigToPub(hash.Bytes(), signature)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ethcrypto.PubkeyToAddress(*publicKey), ethcrypto.PubkeyToAddress(key.PublicKey); got != want {
		t.Fatalf("signer = %s, want %s", got, want)
	}
}

func TestCalculateSaltUsesPackedUint256Words(t *testing.T) {
	core := common.HexToAddress("0x1111111111111111111111111111111111111111")
	got := calculateSalt("Meme", "MEME", core, 10, 11)
	// This is intentionally assembled without calculateSalt: Solidity's
	// abi.encodePacked(uint256) contributes 32 bytes, unlike big.Int.Bytes().
	expectedInput := append([]byte("MemeMEME"), new(big.Int).Set(TotalSupply).FillBytes(make([]byte, 32))...)
	expectedInput = append(expectedInput, core.Bytes()...)
	expectedInput = append(expectedInput, new(big.Int).SetUint64(10).FillBytes(make([]byte, 32))...)
	expectedInput = append(expectedInput, new(big.Int).SetUint64(11).FillBytes(make([]byte, 32))...)
	if want := ethcrypto.Keccak256Hash(expectedInput); got != want {
		t.Fatalf("salt = %s, want %s", got, want)
	}
}
