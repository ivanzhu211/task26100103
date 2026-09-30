package forkchain

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
)

// ---------- independent genesis-replay reference ----------

type expectedState struct {
	bal   map[string]*big.Int
	nonce map[string]uint64
	allow map[Grant]*big.Int
}

func newExpected(genesis map[string]*big.Int) *expectedState {
	st := &expectedState{
		bal:   make(map[string]*big.Int),
		nonce: make(map[string]uint64),
		allow: make(map[Grant]*big.Int),
	}
	for addr, amt := range genesis {
		st.bal[addr] = new(big.Int).Set(amt)
	}
	return st
}

func (st *expectedState) apply(tx Tx) {
	switch tx.Kind {
	case TxApprove:
		st.allow[Grant{Owner: tx.Sender, Spender: tx.Spender}] = new(big.Int).Set(tx.Amount)
		st.nonce[tx.Sender]++
	case TxBatch:
		total := new(big.Int)
		for _, o := range tx.Outputs {
			total.Add(total, o.Amount)
		}
		sub := func(m map[string]*big.Int, k string, v *big.Int) {
			cur := m[k]
			if cur == nil {
				cur = new(big.Int)
			}
			m[k] = new(big.Int).Sub(cur, v)
		}
		add := func(m map[string]*big.Int, k string, v *big.Int) {
			cur := m[k]
			if cur == nil {
				cur = new(big.Int)
			}
			m[k] = new(big.Int).Add(cur, v)
		}
		sub(st.bal, tx.Owner, total)
		g := Grant{Owner: tx.Owner, Spender: tx.Sender}
		cur := st.allow[g]
		if cur == nil {
			cur = new(big.Int)
		}
		st.allow[g] = new(big.Int).Sub(cur, total)
		for _, o := range tx.Outputs {
			add(st.bal, o.To, o.Amount)
		}
		st.nonce[tx.Sender]++
	}
}

// chainRecorder tracks every block the test added so it can replay any chain
// from genesis independently of the kernel.
type chainRecorder struct {
	parents map[string]string
	txs     map[string][]Tx
}

func newRecorder() *chainRecorder {
	return &chainRecorder{parents: map[string]string{}, txs: map[string][]Tx{}}
}

func (r *chainRecorder) record(id, parent string, txs []Tx) {
	r.parents[id] = parent
	r.txs[id] = txs
}

// replay computes the expected state at id by executing the chain from
// genesis in order, with no shared code with the kernel.
func (r *chainRecorder) replay(genesis map[string]*big.Int, genesisID, id string) *expectedState {
	var path []string
	for cur := id; cur != genesisID; cur = r.parents[cur] {
		path = append(path, cur)
	}
	st := newExpected(genesis)
	for i := len(path) - 1; i >= 0; i-- {
		for _, tx := range r.txs[path[i]] {
			st.apply(tx)
		}
	}
	return st
}

func bi(n int64) *big.Int { return big.NewInt(n) }

func checkState(t *testing.T, c *Chain, st *expectedState, accounts []string, grants []Grant) {
	t.Helper()
	_, views, allows := c.Read(accounts, grants)
	for _, addr := range accounts {
		wantBal := st.bal[addr]
		if wantBal == nil {
			wantBal = new(big.Int)
		}
		if views[addr].Balance.Cmp(wantBal) != 0 {
			t.Errorf("account %s balance: got %s, want %s", addr, views[addr].Balance, wantBal)
		}
		if views[addr].Nonce != st.nonce[addr] {
			t.Errorf("account %s nonce: got %d, want %d", addr, views[addr].Nonce, st.nonce[addr])
		}
	}
	for _, g := range grants {
		want := st.allow[g]
		if want == nil {
			want = new(big.Int)
		}
		if allows[g].Cmp(want) != 0 {
			t.Errorf("grant %v allowance: got %s, want %s", g, allows[g], want)
		}
	}
}

// ---------- test 1: forks, allowance spend vs replace, replay cross-check ----------

func TestForkSwitchMatchesReplay(t *testing.T) {
	genesisID := "g"
	genesis := map[string]*big.Int{"A": bi(1000), "B": bi(500)}
	c, err := NewChain(genesisID, genesis)
	if err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()

	add := func(id, parent string, txs []Tx) {
		t.Helper()
		if err := c.AddBlock(id, parent, txs); err != nil {
			t.Fatalf("AddBlock(%s): %v", id, err)
		}
		rec.record(id, parent, txs)
	}

	// Common block: A approves B for 300.
	c1Txs := []Tx{Approve("A", "B", bi(300), 0)}
	add("c1", genesisID, c1Txs)

	// Branch X: B spends part of the A->B allowance. Outputs include a
	// repeated address (C twice) and the owner itself (A).
	x1Txs := []Tx{Batch("B", "A", []Output{
		{To: "C", Amount: bi(100)},
		{To: "C", Amount: bi(50)},
		{To: "A", Amount: bi(30)},
	}, 0)}
	add("x1", "c1", x1Txs)

	// Branch Y: A replaces the allowance with 10 (not additive).
	y1Txs := []Tx{Approve("A", "B", bi(10), 1)}
	add("y1", "c1", y1Txs)

	// z1 is valid under its parent x1 (remaining allowance 120 >= 100) but
	// would be invalid under branch Y's head state (allowance 10 < 100).
	// Move the head to Y first: AddBlock must still validate against x1.
	if err := c.SwitchHead("y1"); err != nil {
		t.Fatal(err)
	}
	z1Txs := []Tx{Batch("B", "A", []Output{{To: "D", Amount: bi(100)}}, 1)}
	add("z1", "x1", z1Txs)
	if c.Head() != "y1" {
		t.Fatalf("AddBlock changed head: got %s, want y1", c.Head())
	}

	// The mirror-image batch on branch Y (allowance only 10) must be rejected.
	if err := c.AddBlock("bad", "y1", []Tx{Batch("B", "A", []Output{{To: "D", Amount: bi(100)}}, 0)}); !errors.Is(err, ErrInsufficientAllowance) {
		t.Fatalf("expected ErrInsufficientAllowance on branch Y, got %v", err)
	}

	accounts := []string{"A", "B", "C", "D", "ghost"}
	grants := []Grant{
		{Owner: "A", Spender: "B"},
		{Owner: "B", Spender: "A"},
		{Owner: "A", Spender: "A"},
		{Owner: "C", Spender: "D"},
	}

	// Repeatedly switch between branches, ancestors and descendants; after
	// every switch the visible state must equal the genesis replay exactly.
	heads := []string{"x1", "y1", "z1", "c1", genesisID, "z1", "y1", "x1", "z1", "y1", "x1"}
	for round := 0; round < 3; round++ {
		for _, h := range heads {
			if err := c.SwitchHead(h); err != nil {
				t.Fatalf("SwitchHead(%s): %v", h, err)
			}
			if c.Head() != h {
				t.Fatalf("head: got %s, want %s", c.Head(), h)
			}
			st := rec.replay(genesis, genesisID, h)
			checkState(t, c, st, accounts, grants)
		}
	}
}

// ---------- test 2: atomic rejection of a partially-valid block ----------

func TestRejectedBlockIsAtomic(t *testing.T) {
	genesisID := "g"
	genesis := map[string]*big.Int{"A": bi(100)}
	c, err := NewChain(genesisID, genesis)
	if err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()

	accounts := []string{"A", "B", "C"}
	grants := []Grant{{Owner: "A", Spender: "B"}}

	// First two txs succeed (approve 50, then spend 30 of it); the last one
	// overspends the remaining balance/allowance and must fail.
	badTxs := []Tx{
		Approve("A", "B", bi(50), 0),
		Batch("B", "A", []Output{{To: "C", Amount: bi(30)}}, 0),
		Batch("B", "A", []Output{{To: "C", Amount: bi(1000)}}, 1),
	}
	if err := c.AddBlock("bad", genesisID, badTxs); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	if c.HasBlock("bad") {
		t.Fatal("rejected block was stored")
	}
	if c.Head() != genesisID {
		t.Fatalf("head moved after rejection: %s", c.Head())
	}
	checkState(t, c, rec.replay(genesis, genesisID, genesisID), accounts, grants)

	// Same again, this time failing on a wrong nonce in the last tx.
	badNonceTxs := []Tx{
		Approve("A", "B", bi(50), 0),
		Approve("A", "C", bi(5), 7),
	}
	if err := c.AddBlock("bad2", genesisID, badNonceTxs); !errors.Is(err, ErrBadNonce) {
		t.Fatalf("expected ErrBadNonce, got %v", err)
	}
	if c.HasBlock("bad2") {
		t.Fatal("rejected block was stored")
	}
	checkState(t, c, rec.replay(genesis, genesisID, genesisID), accounts, grants)

	// A valid block can still be added from the same parent afterwards.
	goodTxs := []Tx{
		Approve("A", "B", bi(50), 0),
		Batch("B", "A", []Output{{To: "C", Amount: bi(30)}}, 0),
	}
	if err := c.AddBlock("good", genesisID, goodTxs); err != nil {
		t.Fatalf("AddBlock(good): %v", err)
	}
	rec.record("good", genesisID, goodTxs)

	// The previously rejected id was not reserved and can be reused.
	if err := c.AddBlock("bad", genesisID, nil); err != nil {
		t.Fatalf("reusing rejected block id failed: %v", err)
	}
	rec.record("bad", genesisID, nil)

	// Parent (genesis) historical state is intact after all of the above.
	if err := c.SwitchHead("good"); err != nil {
		t.Fatal(err)
	}
	checkState(t, c, rec.replay(genesis, genesisID, "good"), accounts, grants)
	if err := c.SwitchHead(genesisID); err != nil {
		t.Fatal(err)
	}
	checkState(t, c, rec.replay(genesis, genesisID, genesisID), accounts, grants)
	if err := c.SwitchHead("bad"); err != nil {
		t.Fatal(err)
	}
	checkState(t, c, rec.replay(genesis, genesisID, "bad"), accounts, grants)
}

// ---------- test 3: switch cost independent of prefix length and accounts ----------

func TestSwitchDoesNotScanPrefixOrAccounts(t *testing.T) {
	const (
		numAccounts = 100000
		prefixLen   = 20000
		branchLen   = 3
	)
	genesisID := "g"
	genesis := make(map[string]*big.Int, numAccounts)
	for i := 0; i < numAccounts; i++ {
		genesis[fmt.Sprintf("acc%d", i)] = bi(10)
	}
	c, err := NewChain(genesisID, genesis)
	if err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()

	// Common prefix: 20000 blocks, each touching one distinct account. The
	// head follows the growing tip so each AddBlock is O(1).
	prev := genesisID
	for i := 0; i < prefixLen; i++ {
		id := fmt.Sprintf("p%d", i)
		txs := []Tx{Approve(fmt.Sprintf("acc%d", i), "sink", bi(1), 0)}
		if err := c.AddBlock(id, prev, txs); err != nil {
			t.Fatalf("AddBlock(%s): %v", id, err)
		}
		rec.record(id, prev, txs)
		if err := c.SwitchHead(id); err != nil {
			t.Fatal(err)
		}
		prev = id
	}

	// Two short branches off the prefix tip, touching disjoint accounts.
	branchAccounts := []string{"acc90000", "acc90001", "acc90002", "acc90003", "acc90004", "acc90005"}
	for side, name := range []string{"a", "b"} {
		parent := prev
		for i := 0; i < branchLen; i++ {
			id := fmt.Sprintf("%s%d", name, i)
			txs := []Tx{Approve(branchAccounts[side*branchLen+i], "sink", bi(2), 0)}
			if err := c.AddBlock(id, parent, txs); err != nil {
				t.Fatalf("AddBlock(%s): %v", id, err)
			}
			rec.record(id, parent, txs)
			parent = id
		}
	}

	// Park the head on the prefix tip before measuring.
	if err := c.SwitchHead(prev); err != nil {
		t.Fatal(err)
	}

	// Switch from the prefix tip to branch A tip.
	c.ResetStats()
	if err := c.SwitchHead("a2"); err != nil {
		t.Fatal(err)
	}
	st := c.Stats()
	if st.BlocksProcessed != branchLen {
		t.Fatalf("switch to a2 processed %d blocks, want %d (prefix len %d, accounts %d)",
			st.BlocksProcessed, branchLen, prefixLen, numAccounts)
	}
	if st.EntriesTouched > 2*branchLen {
		t.Fatalf("switch to a2 touched %d entries, want <= %d", st.EntriesTouched, 2*branchLen)
	}

	// Switch across branches: undo A, redo B. Still tiny and bounded by the
	// branch lengths, not by the 20000-block prefix or the 100000 accounts.
	c.ResetStats()
	if err := c.SwitchHead("b2"); err != nil {
		t.Fatal(err)
	}
	st = c.Stats()
	if st.BlocksProcessed != 2*branchLen {
		t.Fatalf("cross-branch switch processed %d blocks, want %d", st.BlocksProcessed, 2*branchLen)
	}
	if st.EntriesTouched > 4*branchLen {
		t.Fatalf("cross-branch switch touched %d entries, want <= %d", st.EntriesTouched, 4*branchLen)
	}

	// Correctness is still exact after the cheap switches.
	accounts := []string{"acc0", "acc19999", "acc90000", "acc90003", "acc99999", "ghost"}
	grants := []Grant{
		{Owner: "acc0", Spender: "sink"},
		{Owner: "acc90000", Spender: "sink"},
		{Owner: "acc90003", Spender: "sink"},
	}
	checkState(t, c, rec.replay(genesis, genesisID, "b2"), accounts, grants)
	if err := c.SwitchHead("a2"); err != nil {
		t.Fatal(err)
	}
	checkState(t, c, rec.replay(genesis, genesisID, "a2"), accounts, grants)
}

// ---------- genesis supply cap ----------

func TestGenesisSupplyCap(t *testing.T) {
	capMinus1 := new(big.Int).Sub(maxSupply, big.NewInt(1))
	if _, err := NewChain("g", map[string]*big.Int{"A": capMinus1, "B": big.NewInt(1)}); err != nil {
		t.Fatalf("supply exactly 10^27 must be accepted, got %v", err)
	}
	if _, err := NewChain("g", map[string]*big.Int{"A": capMinus1, "B": big.NewInt(2)}); !errors.Is(err, ErrSupplyExceeded) {
		t.Fatalf("supply above 10^27 must be rejected, got %v", err)
	}
	if _, err := NewChain("g", map[string]*big.Int{"A": big.NewInt(-1)}); err == nil {
		t.Fatal("negative genesis balance must be rejected")
	}
}
