package forkchain

import (
	"fmt"
	"math/big"
	"testing"
)

// 100k accounts, 20k common-prefix blocks, two short branches of 3 blocks.
// Switching between the branch tips must only touch the blocks between them
// and the state entries those blocks journal — never the 20k-block prefix
// or the 100k accounts. Proven with exact work counters, not wall time.
func TestShortBranchSwitchDoesNotScanPrefix(t *testing.T) {
	const (
		numAccounts = 100_000
		prefixLen   = 20_000
		branchLen   = 3
	)
	genesis := make(map[string]*big.Int, numAccounts)
	for i := 0; i < numAccounts; i++ {
		genesis[acct(i)] = amt(10)
	}
	k, err := New("G", genesis)
	if err != nil {
		t.Fatal(err)
	}

	// Common prefix: block i holds one Approve by account i (nonce 0).
	// The head follows the tip while building, so each add is O(1).
	prev := "G"
	for i := 0; i < prefixLen; i++ {
		id := fmt.Sprintf("p%d", i)
		err := k.AddBlock(id, prev, []Tx{
			Approve{Sender: acct(i), Spender: "sink", Amount: amt(1), Nonce: 0},
		})
		if err != nil {
			t.Fatalf("prefix block %d: %v", i, err)
		}
		if err := k.SwitchHead(id); err != nil {
			t.Fatal(err)
		}
		prev = id
	}
	tip := prev

	// Branch A: accounts prefixLen .. prefixLen+branchLen-1.
	parent := tip
	for j := 0; j < branchLen; j++ {
		id := fmt.Sprintf("a%d", j)
		err := k.AddBlock(id, parent, []Tx{
			Approve{Sender: acct(prefixLen + j), Spender: "sinkA", Amount: amt(7), Nonce: 0},
		})
		if err != nil {
			t.Fatalf("branch A block %d: %v", j, err)
		}
		parent = id
	}
	// Branch B from the same tip; b2/b3 are added while the head sits at
	// the A tip, i.e. as side-chain validation.
	parent = tip
	for j := 0; j < branchLen; j++ {
		id := fmt.Sprintf("b%d", j)
		err := k.AddBlock(id, parent, []Tx{
			Approve{Sender: acct(prefixLen + branchLen + j), Spender: "sinkB", Amount: amt(9), Nonce: 0},
		})
		if err != nil {
			t.Fatalf("branch B block %d: %v", j, err)
		}
		parent = id
	}

	if err := k.SwitchHead("a2"); err != nil {
		t.Fatal(err)
	}

	// Side-chain validation cost: adding b-blocks above already happened
	// with the head away from their parents; do one more and measure.
	k.ResetStats()
	err = k.AddBlock("b3x", "b2", []Tx{
		Approve{Sender: acct(prefixLen + 2*branchLen), Spender: "sinkB", Amount: amt(9), Nonce: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := k.Stats()
	// Head starts at a2: walk a2->b2 is 2*branchLen blocks, the walk back
	// from the new block b3x is branchLen+1+branchLen. Never the prefix.
	if max := 4*branchLen + 1; s.BlocksProcessed > max {
		t.Fatalf("side-chain AddBlock processed %d blocks, want <= %d", s.BlocksProcessed, max)
	}
	if s.StateEntriesTouched > 100 {
		t.Fatalf("side-chain AddBlock touched %d entries", s.StateEntriesTouched)
	}
	// A nonce that is stale everywhere above p0 must fail on this branch too.
	if err := k.AddBlock("b3bad", "b2", []Tx{
		Approve{Sender: acct(0), Spender: "sinkB", Amount: amt(9), Nonce: 0},
	}); err == nil {
		t.Fatal("stale nonce should fail")
	}

	// Switch A tip (depth prefix+branchLen) -> B tip (depth
	// prefix+branchLen+1): exactly branchLen blocks undone and
	// branchLen+1 redone, each journaling exactly 2 entries
	// (one grant + one nonce).
	k.ResetStats()
	if err := k.SwitchHead("b3x"); err != nil {
		t.Fatal(err)
	}
	s = k.Stats()
	if want := 2*branchLen + 1; s.BlocksProcessed != want {
		t.Fatalf("switch processed %d blocks, want exactly %d (not %d prefix blocks)",
			s.BlocksProcessed, want, prefixLen)
	}
	if want := 2 * (2*branchLen + 1); s.StateEntriesTouched != want {
		t.Fatalf("switch touched %d entries, want exactly %d (not %d accounts)",
			s.StateEntriesTouched, want, numAccounts)
	}

	// And back: symmetric cost.
	k.ResetStats()
	if err := k.SwitchHead("a2"); err != nil {
		t.Fatal(err)
	}
	if s := k.Stats(); s.BlocksProcessed != 2*branchLen+1 {
		t.Fatalf("switch back processed %d blocks", s.BlocksProcessed)
	}

	// State on branch A tip is exactly genesis + prefix + branch A.
	if err := k.SwitchHead("b3x"); err != nil {
		t.Fatal(err)
	}
	v := k.Read([]string{acct(0), acct(prefixLen), acct(prefixLen + 2*branchLen)},
		[]Grant{{acct(0), "sink"}, {acct(prefixLen), "sinkA"}, {acct(prefixLen + 2*branchLen), "sinkB"}})
	if v.Grants[Grant{acct(0), "sink"}].Cmp(amt(1)) != 0 {
		t.Fatal("prefix grant missing on branch B")
	}
	if v.Grants[Grant{acct(prefixLen), "sinkA"}].Sign() != 0 {
		t.Fatal("branch A grant leaked into branch B")
	}
	if v.Grants[Grant{acct(prefixLen + 2*branchLen), "sinkB"}].Cmp(amt(9)) != 0 {
		t.Fatal("branch B grant missing")
	}
}

func acct(i int) string { return fmt.Sprintf("acct%06d", i) }
