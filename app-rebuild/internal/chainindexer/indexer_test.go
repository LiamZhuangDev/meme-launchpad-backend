package chainindexer

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/meme-launchpad/app-rebuild/internal/config"
)

type fakeClient struct {
	chainID *big.Int
	latest  uint64
	query   ethereum.FilterQuery
}

func (c *fakeClient) ChainID(context.Context) (*big.Int, error)   { return c.chainID, nil }
func (c *fakeClient) BlockNumber(context.Context) (uint64, error) { return c.latest, nil }
func (c *fakeClient) FilterLogs(_ context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	c.query = query
	return []types.Log{{BlockNumber: 12, TxHash: common.HexToHash("0x01"), Index: 2}}, nil
}

type fakeStore struct {
	last    uint64
	found   bool
	savedTo uint64
	logs    []types.Log
}

func (s *fakeStore) LastSyncedBlock(context.Context, int64, string) (uint64, bool, error) {
	return s.last, s.found, nil
}
func (s *fakeStore) SaveBlockRange(_ context.Context, _ int64, _ string, end uint64, logs []types.Log) error {
	s.savedTo, s.logs = end, logs
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
