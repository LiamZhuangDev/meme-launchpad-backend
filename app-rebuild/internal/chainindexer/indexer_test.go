package chainindexer

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type fakeClient struct {
	chainID *big.Int
	latest  uint64
	query   ethereum.FilterQuery
	logs    []types.Log
	headers map[uint64]*types.Header
}

func (c *fakeClient) ChainID(context.Context) (*big.Int, error)   { return c.chainID, nil }
func (c *fakeClient) BlockNumber(context.Context) (uint64, error) { return c.latest, nil }
func (c *fakeClient) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	return c.headers[number.Uint64()], nil
}
func (c *fakeClient) FilterLogs(_ context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	c.query = query
	if c.logs != nil {
		return c.logs, nil
	}
	return []types.Log{{BlockNumber: 12, TxHash: common.HexToHash("0x01"), Index: 2}}, nil
}

type fakeStore struct {
	last    uint64
	found   bool
	savedTo uint64
	logs    []types.Log
	events  []repository.ChainProjection
}

func (s *fakeStore) LastSyncedBlock(context.Context, int64, string) (uint64, bool, error) {
	return s.last, s.found, nil
}
func (s *fakeStore) SaveBlockRange(_ context.Context, _ int64, _ string, end uint64, logs []types.Log, events []repository.ChainProjection) error {
	s.savedTo, s.logs, s.events = end, logs, events
	return nil
}

func TestSyncAvailableFiltersCoreAndAtomicallyAdvancesRange(t *testing.T) {
	client := &fakeClient{chainID: big.NewInt(97), latest: 12}
	store := &fakeStore{}
	cfg := config.IndexerConfig{RPCURL: "http://rpc.example", ChainID: 97, CoreContract: "0x1111111111111111111111111111111111111111", StartBlock: 10, BlockBatchSize: 10, PollInterval: 1}
	indexer, err := New(client, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	next := uint64(10)
	if err := indexer.syncAvailable(context.Background(), &next); err != nil {
		t.Fatal(err)
	}
	if got := client.query.Addresses; len(got) != 1 || got[0] != common.HexToAddress(cfg.CoreContract) {
		t.Fatalf("addresses = %v", got)
	}
	if client.query.FromBlock.Uint64() != 10 || client.query.ToBlock.Uint64() != 12 {
		t.Fatalf("range = %d-%d", client.query.FromBlock, client.query.ToBlock)
	}
	if store.savedTo != 12 || len(store.logs) != 1 || next != 13 {
		t.Fatalf("save=%d logs=%d next=%d", store.savedTo, len(store.logs), next)
	}
}

func TestSyncAvailableProjectsTokenBoughtLog(t *testing.T) {
	token := common.HexToAddress("0x2222222222222222222222222222222222222222")
	buyer := common.HexToAddress("0x3333333333333333333333333333333333333333")
	client := &fakeClient{
		chainID: big.NewInt(97),
		latest:  15,
		headers: map[uint64]*types.Header{15: &types.Header{Time: 1_700_000_000}},
	}
	store := &fakeStore{}
	cfg := config.IndexerConfig{RPCURL: "http://rpc.example", ChainID: 97, CoreContract: "0x1111111111111111111111111111111111111111", StartBlock: 15, BlockBatchSize: 1, PollInterval: 1}
	indexer, err := New(client, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := indexer.contractABI.Events["TokenBought"].Inputs.NonIndexed().Pack(
		big.NewInt(100),
		big.NewInt(50),
		big.NewInt(3),
		big.NewInt(1100),
		big.NewInt(950),
		big.NewInt(900),
		big.NewInt(100),
	)
	if err != nil {
		t.Fatal(err)
	}
	client.logs = []types.Log{{
		Topics:      []common.Hash{indexer.contractABI.Events["TokenBought"].ID, common.BytesToHash(token.Bytes()), common.BytesToHash(buyer.Bytes())},
		Data:        data,
		BlockNumber: 15,
		TxHash:      common.HexToHash("0xabc"),
		Index:       7,
	}}
	next := uint64(15)
	if err := indexer.syncAvailable(context.Background(), &next); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || store.events[0].TokenBought == nil {
		t.Fatalf("events = %#v", store.events)
	}
	event := store.events[0].TokenBought
	if event.TokenAddress != token.Hex() || event.UserAddress != buyer.Hex() || event.TradeType != repository.TradeTypeBuy {
		t.Fatalf("projected trade = %#v", event)
	}
	if event.BNBAmount.String() != "100" || event.TokenAmount.String() != "50" || event.CollectedBNB.String() != "100" {
		t.Fatalf("amounts = bnb %s token %s collected %s", event.BNBAmount, event.TokenAmount, event.CollectedBNB)
	}
	if !event.BlockTimestamp.Equal(time.Unix(1_700_000_000, 0).UTC()) {
		t.Fatalf("block timestamp = %s", event.BlockTimestamp)
	}
}

func TestVerifyChainRejectsWrongNetwork(t *testing.T) {
	client := &fakeClient{chainID: big.NewInt(1)}
	store := &fakeStore{}
	indexer, err := New(client, store, config.IndexerConfig{RPCURL: "http://rpc.example", ChainID: 97, CoreContract: "0x1111111111111111111111111111111111111111", BlockBatchSize: 1, PollInterval: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.VerifyChain(context.Background()); err == nil {
		t.Fatal("VerifyChain() error = nil")
	}
}
