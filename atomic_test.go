package forkchain

import (
	"errors"
	"math/big"
	"testing"
)

// A block whose early txs succeed but whose last tx fails must be rejected
// as a whole: nothing is stored, the current head and the parent's
// historical state are untouched, and the parent still accepts valid blocks.
func TestInvalidBlockLeavesNoTrace(t *testing.T) {
	genesis := map[string]*big.Int{"O": amt(1000), "S": amt(0)}
	k, err := New("G", genesis)
	if err != nil {
		t.Fatal(err)
	}
	setup := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(300), Nonce: 0}}
	if err := k.AddBlock("p", "G", setup); err != nil {
		t.Fatal(err)
	}
	if err := k.SwitchHead("p"); err != nil {
		t.Fatal(err)
	}

	bad := []Tx{
		// These two succeed and mutate allowance + balances...
		Approve{Sender: "O", Spender: "S", Amount: amt(900), Nonce: 1},
		Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: []Output{
			{To: "A", Amount: amt(100)},
		}},
		// ...but the last one fails: owner balance is 900, needs 950.
		Batch{Sender: "S", Owner: "O", Nonce: 1, Outputs: []Output{
			{To: "B", Amount: amt(950)},
		}},
	}
	before := k.Read(accts, allGrants)
	if err := k.AddBlock("bad", "p", bad); !errors.Is(err, ErrInsufficientBal) {
		t.Fatalf("want ErrInsufficientBal, got %v", err)
	}
	if _, err := k.blocks["bad"]; err {
		t.Fatal("rejected block was stored")
	}

	// Current head and visible state unchanged.
	after := k.Read(accts, allGrants)
	if after.Head != "p" || before.Head != "p" {
		t.Fatalf("head moved: before=%q after=%q", before.Head, after.Head)
	}
	for _, a := range accts {
		if before.Balances[a].Cmp(after.Balances[a]) != 0 ||
			before.Nonces[a] != after.Nonces[a] {
			t.Fatalf("state polluted for %s", a)
		}
	}
	for _, g := range allGrants {
		if before.Grants[g].Cmp(after.Grants[g]) != 0 {
			t.Fatalf("grant %v polluted", g)
		}
	}

	// Parent's historical state unchanged: a block that is valid at p but
	// would fail under the partial effects of `bad` (allowance 900) must
	// still validate against the original allowance of 300.
	good := []Tx{Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: []Output{
		{To: "A", Amount: amt(250)},
	}}}
	if err := k.AddBlock("good", "p", good); err != nil {
		t.Fatalf("parent history polluted: %v", err)
	}
	if err := k.SwitchHead("good"); err != nil {
		t.Fatal(err)
	}
	r := newReplay(genesis)
	r.run(setup, good)
	checkView(t, k, r, "good", accts, allGrants)
}

// Same as above but the failing tx has a wrong nonce.
func TestInvalidBlockBadNonce(t *testing.T) {
	genesis := map[string]*big.Int{"O": amt(100)}
	k, err := New("G", genesis)
	if err != nil {
		t.Fatal(err)
	}
	bad := []Tx{
		Approve{Sender: "O", Spender: "S", Amount: amt(50), Nonce: 0},
		Approve{Sender: "O", Spender: "S", Amount: amt(60), Nonce: 5}, // wrong
	}
	if err := k.AddBlock("bad", "G", bad); !errors.Is(err, ErrBadNonce) {
		t.Fatalf("want ErrBadNonce, got %v", err)
	}
	v := k.Read(accts, allGrants)
	if v.Head != "G" || v.Grants[grantOS].Sign() != 0 || v.Nonces["O"] != 0 {
		t.Fatalf("genesis state polluted: %+v", v)
	}
	if err := k.AddBlock("ok", "G", bad[:1]); err != nil {
		t.Fatalf("valid block on same parent rejected: %v", err)
	}
}
