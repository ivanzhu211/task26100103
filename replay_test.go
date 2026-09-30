package forkchain

import (
	"math/big"
	"testing"
)

// replayState is an independent, deliberately simple reference model: it
// executes transactions sequentially from genesis along one chain. It shares
// no code with the kernel's journal/switch machinery.
type replayState struct {
	balances map[string]*big.Int
	nonces   map[string]uint64
	grants   map[Grant]*big.Int
}

func newReplay(genesis map[string]*big.Int) *replayState {
	r := &replayState{
		balances: make(map[string]*big.Int),
		nonces:   make(map[string]uint64),
		grants:   make(map[Grant]*big.Int),
	}
	for a, b := range genesis {
		r.balances[a] = new(big.Int).Set(b)
	}
	return r
}

func (r *replayState) bal(a string) *big.Int {
	if v, ok := r.balances[a]; ok {
		return v
	}
	return new(big.Int)
}

func (r *replayState) grant(g Grant) *big.Int {
	if v, ok := r.grants[g]; ok {
		return v
	}
	return new(big.Int)
}

// exec applies one tx; returns false on any validation failure.
func (r *replayState) exec(tx Tx) bool {
	switch t := tx.(type) {
	case Approve:
		if t.Amount == nil || t.Amount.Sign() < 0 || r.nonces[t.Sender] != t.Nonce {
			return false
		}
		r.grants[Grant{t.Sender, t.Spender}] = new(big.Int).Set(t.Amount)
		r.nonces[t.Sender]++
	case Batch:
		if len(t.Outputs) == 0 || len(t.Outputs) > MaxOutputsPerBatch ||
			r.nonces[t.Sender] != t.Nonce {
			return false
		}
		total := new(big.Int)
		for _, o := range t.Outputs {
			if o.Amount == nil || o.Amount.Sign() <= 0 {
				return false
			}
			total.Add(total, o.Amount)
		}
		g := Grant{t.Owner, t.Sender}
		if r.bal(t.Owner).Cmp(total) < 0 || r.grant(g).Cmp(total) < 0 {
			return false
		}
		r.balances[t.Owner] = new(big.Int).Sub(r.bal(t.Owner), total)
		r.grants[g] = new(big.Int).Sub(r.grant(g), total)
		for _, o := range t.Outputs {
			r.balances[o.To] = new(big.Int).Add(r.bal(o.To), o.Amount)
		}
		r.nonces[t.Sender]++
	default:
		return false
	}
	return true
}

// run executes blocks' txs in chain order; each argument is one block.
func (r *replayState) run(blocks ...[]Tx) {
	for _, txs := range blocks {
		for _, tx := range txs {
			if !r.exec(tx) {
				panic("replay: invalid tx in reference chain")
			}
		}
	}
}

// checkView compares the kernel's visible state against the reference for
// the given accounts and grants.
func checkView(t *testing.T, k *Kernel, r *replayState, head string, accounts []string, grants []Grant) {
	t.Helper()
	v := k.Read(accounts, grants)
	if v.Head != head {
		t.Fatalf("head: got %q want %q", v.Head, head)
	}
	for _, a := range accounts {
		if v.Balances[a].Cmp(r.bal(a)) != 0 {
			t.Fatalf("balance[%s]: got %s want %s", a, v.Balances[a], r.bal(a))
		}
		if v.Nonces[a] != r.nonces[a] {
			t.Fatalf("nonce[%s]: got %d want %d", a, v.Nonces[a], r.nonces[a])
		}
	}
	for _, g := range grants {
		if v.Grants[g].Cmp(r.grant(g)) != 0 {
			t.Fatalf("grant[%v]: got %s want %s", g, v.Grants[g], r.grant(g))
		}
	}
}

func amt(v int64) *big.Int { return big.NewInt(v) }
