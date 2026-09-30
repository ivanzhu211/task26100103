package forkchain

import (
	"errors"
	"math/big"
	"testing"
)

func TestGenesisSupplyCap(t *testing.T) {
	over := new(big.Int).Add(MaxTotalSupply, big.NewInt(1))
	if _, err := New("G", map[string]*big.Int{"O": over}); !errors.Is(err, ErrGenesisTooLarge) {
		t.Fatalf("want ErrGenesisTooLarge, got %v", err)
	}
	exact := new(big.Int).Set(MaxTotalSupply)
	if _, err := New("G", map[string]*big.Int{"O": exact}); err != nil {
		t.Fatalf("exactly 10^27 must be allowed: %v", err)
	}
	if _, err := New("G", map[string]*big.Int{"O": big.NewInt(-1)}); !errors.Is(err, ErrNegativeGenesis) {
		t.Fatalf("want ErrNegativeGenesis, got %v", err)
	}
}

func TestBlockLimits(t *testing.T) {
	k, err := New("G", map[string]*big.Int{"O": amt(10)})
	if err != nil {
		t.Fatal(err)
	}
	txs := make([]Tx, MaxTxsPerBlock+1)
	for i := range txs {
		txs[i] = Approve{Sender: "O", Spender: "S", Amount: amt(1), Nonce: 0}
	}
	if err := k.AddBlock("big", "G", txs); !errors.Is(err, ErrTooManyTxs) {
		t.Fatalf("want ErrTooManyTxs, got %v", err)
	}
	if err := k.AddBlock("big", "G", txs); !errors.Is(err, ErrTooManyTxs) {
		t.Fatal("rejected oversized block must not be stored")
	}
	if err := k.AddBlock("G", "G", nil); !errors.Is(err, ErrDuplicateBlock) {
		t.Fatalf("genesis id reuse: want ErrDuplicateBlock, got %v", err)
	}
	if err := k.AddBlock("x", "nope", nil); !errors.Is(err, ErrUnknownParent) {
		t.Fatalf("want ErrUnknownParent, got %v", err)
	}
	if err := k.SwitchHead("nope"); !errors.Is(err, ErrUnknownBlock) {
		t.Fatalf("want ErrUnknownBlock, got %v", err)
	}
}

func TestApproveReplacesNotAccumulates(t *testing.T) {
	k, err := New("G", map[string]*big.Int{"O": amt(10)})
	if err != nil {
		t.Fatal(err)
	}
	b1 := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(100), Nonce: 0}}
	b2 := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(30), Nonce: 1}}
	if err := k.AddBlock("b1", "G", b1); err != nil {
		t.Fatal(err)
	}
	if err := k.AddBlock("b2", "b1", b2); err != nil {
		t.Fatal(err)
	}
	if err := k.SwitchHead("b2"); err != nil {
		t.Fatal(err)
	}
	v := k.Read(nil, []Grant{grantOS})
	if v.Grants[grantOS].Cmp(amt(30)) != 0 {
		t.Fatalf("approve must replace: got %s want 30", v.Grants[grantOS])
	}
}

func TestBatchRules(t *testing.T) {
	k, err := New("G", map[string]*big.Int{"O": amt(1000)})
	if err != nil {
		t.Fatal(err)
	}
	// sender == owner does not skip the allowance check.
	selfBatch := []Tx{Batch{Sender: "O", Owner: "O", Nonce: 0, Outputs: []Output{
		{To: "A", Amount: amt(5)},
	}}}
	if err := k.AddBlock("self", "G", selfBatch); !errors.Is(err, ErrInsufficientGrnt) {
		t.Fatalf("sender==owner must still need allowance: got %v", err)
	}
	// Empty outputs and non-positive amounts are rejected.
	empty := []Tx{Batch{Sender: "O", Owner: "O", Nonce: 0}}
	if err := k.AddBlock("empty", "G", empty); !errors.Is(err, ErrBadOutputCount) {
		t.Fatalf("want ErrBadOutputCount, got %v", err)
	}
	zero := []Tx{Batch{Sender: "O", Owner: "O", Nonce: 0, Outputs: []Output{
		{To: "A", Amount: amt(0)},
	}}}
	if err := k.AddBlock("zero", "G", zero); !errors.Is(err, ErrNonPositiveOut) {
		t.Fatalf("want ErrNonPositiveOut, got %v", err)
	}
	// 501 outputs rejected, 500 accepted (with allowance in place).
	mk := func(n int) []Tx {
		outs := make([]Output, n)
		for i := range outs {
			outs[i] = Output{To: "A", Amount: amt(1)}
		}
		return []Tx{
			Approve{Sender: "O", Spender: "S", Amount: amt(1000), Nonce: 0},
			Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: outs},
		}
	}
	if err := k.AddBlock("o501", "G", mk(501)); !errors.Is(err, ErrBadOutputCount) {
		t.Fatalf("501 outputs: want ErrBadOutputCount, got %v", err)
	}
	if err := k.AddBlock("o500", "G", mk(500)); err != nil {
		t.Fatalf("500 outputs must be accepted: %v", err)
	}
	// Negative approve rejected.
	neg := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(-1), Nonce: 0}}
	if err := k.AddBlock("neg", "G", neg); !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("want ErrNegativeAmount, got %v", err)
	}
	// Unseen entries read as zero.
	v := k.Read([]string{"ghost"}, []Grant{{"ghost", "ghost2"}})
	if v.Balances["ghost"].Sign() != 0 || v.Nonces["ghost"] != 0 ||
		v.Grants[Grant{"ghost", "ghost2"}].Sign() != 0 {
		t.Fatal("unseen entries must read as zero")
	}
}
