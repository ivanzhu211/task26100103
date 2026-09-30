package forkchain

import (
	"math/big"
	"testing"
)

var (
	accts     = []string{"O", "S", "A", "B", "nobody"}
	grantOS   = Grant{"O", "S"}
	grantSO   = Grant{"S", "O"}
	grantOA   = Grant{"O", "A"}
	allGrants = []Grant{grantOS, grantSO, grantOA}
)

// Two branches over one token: branch X grants and spends an allowance,
// branch Y grants a smaller allowance and spends less. A later block z1 is
// valid at its parent (x2) but invalid at the then-current head (y2). Batch
// outputs include a repeated address and the owner itself. Heads are
// switched repeatedly and checked against an independent genesis replay.
func TestForkSwitchMatchesReplay(t *testing.T) {
	genesis := map[string]*big.Int{"O": amt(1000)}

	x1 := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(300), Nonce: 0}}
	x2 := []Tx{Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: []Output{
		{To: "A", Amount: amt(100)},
		{To: "A", Amount: amt(50)}, // repeated address
		{To: "O", Amount: amt(30)}, // owner pays itself
	}}} // total 180; needs grant O->S >= 180
	y1 := []Tx{Approve{Sender: "O", Spender: "S", Amount: amt(50), Nonce: 0}}
	y2 := []Tx{Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: []Output{
		{To: "B", Amount: amt(40)},
	}}} // total 40
	// Valid at x2 (grant 120, balance 820) but invalid at y2 (grant 10).
	z1 := []Tx{Batch{Sender: "S", Owner: "O", Nonce: 1, Outputs: []Output{
		{To: "B", Amount: amt(100)},
	}}}

	k, err := New("G", genesis)
	if err != nil {
		t.Fatal(err)
	}
	mustAdd := func(id, parent string, txs []Tx) {
		t.Helper()
		if err := k.AddBlock(id, parent, txs); err != nil {
			t.Fatalf("AddBlock(%s): %v", id, err)
		}
	}
	mustAdd("x1", "G", x1)
	mustAdd("x2", "x1", x2)
	mustAdd("y1", "G", y1)
	mustAdd("y2", "y1", y2)

	// Head is still G; move to y2 so that z1's parent state differs from
	// the visible head state.
	if err := k.SwitchHead("y2"); err != nil {
		t.Fatal(err)
	}
	mustAdd("z1", "x2", z1) // legal at parent x2, illegal at head y2
	if h := k.Read(nil, nil).Head; h != "y2" {
		t.Fatalf("AddBlock moved head to %q", h)
	}

	// Reference states per chain, computed by independent replay.
	ref := func(blocks ...[]Tx) *replayState {
		r := newReplay(genesis)
		r.run(blocks...)
		return r
	}
	rG := ref()
	rX1 := ref(x1)
	rX2 := ref(x1, x2)
	rY2 := ref(y1, y2)
	rZ1 := ref(x1, x2, z1)

	steps := []struct {
		head string
		ref  *replayState
	}{
		{"x2", rX2},
		{"y2", rY2},
		{"z1", rZ1},
		{"G", rG},
		{"x1", rX1},
		{"z1", rZ1},
		{"y2", rY2},
		{"x2", rX2},
		{"G", rG},
		{"z1", rZ1},
	}
	for i, s := range steps {
		if err := k.SwitchHead(s.head); err != nil {
			t.Fatalf("step %d: SwitchHead(%s): %v", i, s.head, err)
		}
		checkView(t, k, s.ref, s.head, accts, allGrants)
	}
}

// Sanity: replay disagrees with itself never; grant consumption and nonce
// bumps on a retracted branch must vanish completely.
func TestRetractedBranchLeavesNoTrace(t *testing.T) {
	genesis := map[string]*big.Int{"O": amt(500)}
	k, err := New("G", genesis)
	if err != nil {
		t.Fatal(err)
	}
	b1 := []Tx{
		Approve{Sender: "O", Spender: "S", Amount: amt(200), Nonce: 0},
		Batch{Sender: "S", Owner: "O", Nonce: 0, Outputs: []Output{
			{To: "S", Amount: amt(200)},
		}},
	}
	if err := k.AddBlock("b1", "G", b1); err != nil {
		t.Fatal(err)
	}
	if err := k.SwitchHead("b1"); err != nil {
		t.Fatal(err)
	}
	checkView(t, k, func() *replayState {
		r := newReplay(genesis)
		r.run(b1)
		return r
	}(), "b1", accts, allGrants)

	// Retract: allowance spend and nonce bumps must disappear.
	if err := k.SwitchHead("G"); err != nil {
		t.Fatal(err)
	}
	checkView(t, k, newReplay(genesis), "G", accts, allGrants)

	// Reactivate: everything must be fully restored.
	if err := k.SwitchHead("b1"); err != nil {
		t.Fatal(err)
	}
	r := newReplay(genesis)
	r.run(b1)
	checkView(t, k, r, "b1", accts, allGrants)
}
