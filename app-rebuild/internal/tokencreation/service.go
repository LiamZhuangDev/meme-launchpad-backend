// Package tokencreation creates the signed intent accepted by MEMECore.createToken.
package tokencreation

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

var (
	TotalSupply         = new(big.Int).Mul(big.NewInt(1_000_000_000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	SaleAmount          = new(big.Int).Mul(big.NewInt(800_000_000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	VirtualBNBReserve   = big.NewInt(8_219_178_082_191_780_000)
	VirtualTokenReserve = new(big.Int).Mul(big.NewInt(1_073_972_602), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
)

const maxInitialBuyPercentage = 9990

type Config struct {
	ChainID               int64
	Core, Factory         common.Address
	TokenCreationBytecode []byte
	Signer                *ecdsa.PrivateKey
}

type Store interface {
	Create(context.Context, repository.TokenCreationRequest) error
}

type Service struct {
	cfg   Config
	store Store
}

type Request struct {
	Name, Symbol         string
	Creator              common.Address
	LaunchTime           uint64
	InitialBuyPercentage uint64
}

type Response struct {
	Data             string `json:"createArg"`
	Signature        string `json:"signature"`
	RequestID        string `json:"requestId"`
	Salt             string `json:"create2Salt"`
	PredictedAddress string `json:"predictedAddress"`
	Nonce            uint64 `json:"nonce"`
	Timestamp        uint64 `json:"timestamp"`
}

type vesting struct {
	Amount     *big.Int `abi:"amount"`
	LaunchTime *big.Int `abi:"launchTime"`
	Duration   *big.Int `abi:"duration"`
	Mode       uint8    `abi:"mode"`
}

// params exactly mirrors IMEMECore.CreateTokenParams. The ABI tags are part of
// the contract boundary: go-ethereum uses them to match tuple components.
type params struct {
	Name                 string         `abi:"name"`
	Symbol               string         `abi:"symbol"`
	TotalSupply          *big.Int       `abi:"totalSupply"`
	SaleAmount           *big.Int       `abi:"saleAmount"`
	VirtualBNBReserve    *big.Int       `abi:"virtualBNBReserve"`
	VirtualTokenReserve  *big.Int       `abi:"virtualTokenReserve"`
	LaunchTime           *big.Int       `abi:"launchTime"`
	Creator              common.Address `abi:"creator"`
	Timestamp            *big.Int       `abi:"timestamp"`
	RequestID            [32]byte       `abi:"requestId"`
	Nonce                *big.Int       `abi:"nonce"`
	InitialBuyPercentage *big.Int       `abi:"initialBuyPercentage"`
	MarginBNB            *big.Int       `abi:"marginBnb"`
	MarginTime           *big.Int       `abi:"marginTime"`
	VestingAllocations   []vesting      `abi:"vestingAllocations"`
}

func New(cfg Config, store Store) (*Service, error) {
	if cfg.Signer == nil || cfg.ChainID < 1 || cfg.Core == (common.Address{}) || cfg.Factory == (common.Address{}) || len(cfg.TokenCreationBytecode) == 0 {
		return nil, errors.New("token creation is not fully configured")
	}
	if store == nil {
		return nil, errors.New("token creation store is required")
	}
	return &Service{cfg: cfg, store: store}, nil
}

func (s *Service) Create(ctx context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	timestamp := uint64(time.Now().Unix())
	nonce, err := randomUint64()
	if err != nil {
		return Response{}, fmt.Errorf("generate nonce: %w", err)
	}
	requestID := ethcrypto.Keccak256Hash([]byte("meme-launchpad:"), request.Creator.Bytes(), uint256Bytes(timestamp), uint256Bytes(nonce))
	var id [32]byte
	copy(id[:], requestID.Bytes())
	p := params{
		Name: request.Name, Symbol: request.Symbol, TotalSupply: TotalSupply, SaleAmount: SaleAmount,
		VirtualBNBReserve: VirtualBNBReserve, VirtualTokenReserve: VirtualTokenReserve,
		LaunchTime: new(big.Int).SetUint64(request.LaunchTime), Creator: request.Creator,
		Timestamp: new(big.Int).SetUint64(timestamp), RequestID: id, Nonce: new(big.Int).SetUint64(nonce),
		InitialBuyPercentage: new(big.Int).SetUint64(request.InitialBuyPercentage), MarginBNB: big.NewInt(0),
		MarginTime: big.NewInt(0), VestingAllocations: []vesting{},
	}
	data, err := encode(p)
	if err != nil {
		return Response{}, fmt.Errorf("ABI encode create-token parameters: %w", err)
	}
	salt := calculateSalt(request.Name, request.Symbol, s.cfg.Core, timestamp, nonce)
	predicted, err := calculateAddress(s.cfg.Factory, s.cfg.TokenCreationBytecode, salt, request.Name, request.Symbol, s.cfg.Core)
	if err != nil {
		return Response{}, err
	}
	hash := ethcrypto.Keccak256Hash(data, uint256Bytes(uint64(s.cfg.ChainID)), s.cfg.Core.Bytes())
	signature, err := ethcrypto.Sign(hash.Bytes(), s.cfg.Signer)
	if err != nil {
		return Response{}, fmt.Errorf("sign create-token parameters: %w", err)
	}
	// OpenZeppelin ECDSA.recover accepts the Ethereum 27/28 recovery form.
	signature[64] += 27
	response := Response{
		Data: "0x" + hex.EncodeToString(data), Signature: "0x" + hex.EncodeToString(signature),
		RequestID: requestID.Hex(), Salt: salt.Hex(), PredictedAddress: predicted.Hex(), Nonce: nonce, Timestamp: timestamp,
	}
	if err := s.store.Create(ctx, repository.TokenCreationRequest{
		RequestID: response.RequestID, CreatorAddress: request.Creator.Hex(), Name: request.Name, Symbol: request.Symbol,
		Data: response.Data, Signature: response.Signature, Salt: response.Salt, PredictedAddress: response.PredictedAddress,
		Nonce: nonce, Timestamp: timestamp,
	}); err != nil {
		return Response{}, fmt.Errorf("persist token creation intent: %w", err)
	}
	return response, nil
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Symbol) == "" || request.Creator == (common.Address{}) {
		return errors.New("name, symbol, and creator are required")
	}
	if len(request.Name) > 100 || len(request.Symbol) > 50 {
		return errors.New("name must be at most 100 characters and symbol at most 50 characters")
	}
	if request.InitialBuyPercentage > maxInitialBuyPercentage {
		return fmt.Errorf("initialBuyPercentage must be from 0 to %d", maxInitialBuyPercentage)
	}
	return nil
}

func encode(value params) ([]byte, error) {
	tuple, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "name", Type: "string"}, {Name: "symbol", Type: "string"}, {Name: "totalSupply", Type: "uint256"},
		{Name: "saleAmount", Type: "uint256"}, {Name: "virtualBNBReserve", Type: "uint256"}, {Name: "virtualTokenReserve", Type: "uint256"},
		{Name: "launchTime", Type: "uint256"}, {Name: "creator", Type: "address"}, {Name: "timestamp", Type: "uint256"},
		{Name: "requestId", Type: "bytes32"}, {Name: "nonce", Type: "uint256"}, {Name: "initialBuyPercentage", Type: "uint256"},
		{Name: "marginBnb", Type: "uint256"}, {Name: "marginTime", Type: "uint256"},
		{Name: "vestingAllocations", Type: "tuple[]", Components: []abi.ArgumentMarshaling{{Name: "amount", Type: "uint256"}, {Name: "launchTime", Type: "uint256"}, {Name: "duration", Type: "uint256"}, {Name: "mode", Type: "uint8"}}},
	})
	if err != nil {
		return nil, err
	}
	return abi.Arguments{{Type: tuple}}.Pack(value)
}

// MEMEFactory uses keccak256(abi.encodePacked(name, symbol, totalSupply,
// metaNode, timestamp, nonce)); uint256 values are fixed 32-byte words here.
func calculateSalt(name, symbol string, core common.Address, timestamp, nonce uint64) common.Hash {
	return ethcrypto.Keccak256Hash([]byte(name), []byte(symbol), uint256BytesFromBig(TotalSupply), core.Bytes(), uint256Bytes(timestamp), uint256Bytes(nonce))
}

func calculateAddress(factory common.Address, bytecode []byte, salt common.Hash, name, symbol string, core common.Address) (common.Address, error) {
	constructor, err := abi.Arguments{{Type: mustType("string")}, {Type: mustType("string")}, {Type: mustType("uint256")}, {Type: mustType("address")}}.Pack(name, symbol, TotalSupply, core)
	if err != nil {
		return common.Address{}, fmt.Errorf("ABI encode token constructor: %w", err)
	}
	initHash := ethcrypto.Keccak256(append(append([]byte{}, bytecode...), constructor...))
	return common.BytesToAddress(ethcrypto.Keccak256([]byte{0xff}, factory.Bytes(), salt.Bytes(), initHash)[12:]), nil
}

func uint256Bytes(value uint64) []byte          { return uint256BytesFromBig(new(big.Int).SetUint64(value)) }
func uint256BytesFromBig(value *big.Int) []byte { return value.FillBytes(make([]byte, 32)) }
func mustType(name string) abi.Type {
	t, err := abi.NewType(name, "", nil)
	if err != nil {
		panic(err)
	}
	return t
}
func randomUint64() (uint64, error) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	return new(big.Int).SetBytes(b).Uint64(), err
}

func ParseBytecode(value string) ([]byte, error) {
	bytecode, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(bytecode) == 0 {
		return nil, errors.New("TOKEN_CREATION_BYTECODE must be non-empty hexadecimal bytecode")
	}
	return bytecode, nil
}
