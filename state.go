package forkchain

import "math/big"

// state is the single mutable view of the current head. Values stored in the
// maps are never mutated in place; they are always replaced, so journaled
// old/new pointers stay valid forever.
type state struct {
	balances map[string]*big.Int
	nonces   map[string]uint64
	grants   map[Grant]*big.Int
}

func newState() *state {
	return &state{
		balances: make(map[string]*big.Int),
		nonces:   make(map[string]uint64),
		grants:   make(map[Grant]*big.Int),
	}
}

func (s *state) balance(addr string) *big.Int {
	if v, ok := s.balances[addr]; ok {
		return v
	}
	return new(big.Int)
}

func (s *state) nonce(addr string) uint64 { return s.nonces[addr] }

func (s *state) grant(g Grant) *big.Int {
	if v, ok := s.grants[g]; ok {
		return v
	}
	return new(big.Int)
}

type changeKind uint8

const (
	ckBalance changeKind = iota
	ckNonce
	ckGrant
)

// change journals one state entry mutation (old -> new).
type change struct {
	kind     changeKind
	addr     string // balance / nonce key
	grant    Grant  // grant key
	oldVal   *big.Int
	newVal   *big.Int
	oldNonce uint64
	newNonce uint64
}

// setBalance records and applies a balance change.
func (s *state) setBalance(addr string, v *big.Int, log *[]change) {
	old := s.balance(addr)
	*log = append(*log, change{kind: ckBalance, addr: addr, oldVal: old, newVal: v})
	s.balances[addr] = v
}

// setNonce records and applies a nonce change.
func (s *state) setNonce(addr string, v uint64, log *[]change) {
	old := s.nonces[addr]
	*log = append(*log, change{kind: ckNonce, addr: addr, oldNonce: old, newNonce: v})
	s.nonces[addr] = v
}

// setGrant records and applies a grant change.
func (s *state) setGrant(g Grant, v *big.Int, log *[]change) {
	old := s.grant(g)
	*log = append(*log, change{kind: ckGrant, grant: g, oldVal: old, newVal: v})
	s.grants[g] = v
}

// apply replays one journaled change forward.
func (s *state) apply(c change) {
	switch c.kind {
	case ckBalance:
		s.balances[c.addr] = c.newVal
	case ckNonce:
		s.nonces[c.addr] = c.newNonce
	case ckGrant:
		s.grants[c.grant] = c.newVal
	}
}

// revert restores one journaled change to its old value.
func (s *state) revert(c change) {
	switch c.kind {
	case ckBalance:
		s.balances[c.addr] = c.oldVal
	case ckNonce:
		s.nonces[c.addr] = c.oldNonce
	case ckGrant:
		s.grants[c.grant] = c.oldVal
	}
}

// revertAll rolls back a partial journal in reverse order.
func (s *state) revertAll(log []change) {
	for i := len(log) - 1; i >= 0; i-- {
		s.revert(log[i])
	}
}

// block is an immutable block with its change journal.
type block struct {
	id      string
	parent  string
	depth   int
	changes []change
}
