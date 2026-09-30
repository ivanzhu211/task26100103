package forkchain

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrDuplicateBlock  = errors.New("forkchain: duplicate block id")
	ErrUnknownParent   = errors.New("forkchain: unknown parent block")
	ErrUnknownBlock    = errors.New("forkchain: unknown block")
	ErrTooManyTxs      = errors.New("forkchain: too many transactions in block")
	ErrNegativeGenesis = errors.New("forkchain: negative genesis balance")
	ErrGenesisTooLarge = errors.New("forkchain: genesis total exceeds 10^27")
	errEmptyGenesisID  = errors.New("forkchain: empty genesis id")
)

// Kernel is the fork-state kernel. Calls are serial; not safe for concurrent
// use. All state lives in memory.
type Kernel struct {
	st     *state
	blocks map[string]*block
	head   string
	stats  Stats
}

// New creates a kernel with the given genesis block id and balances. The
// genesis block is kept forever and can never be removed or replaced.
func New(genesisID string, balances map[string]*big.Int) (*Kernel, error) {
	if genesisID == "" {
		return nil, errEmptyGenesisID
	}
	st := newState()
	total := new(big.Int)
	for addr, bal := range balances {
		if bal == nil || bal.Sign() < 0 {
			return nil, fmt.Errorf("%w: %q", ErrNegativeGenesis, addr)
		}
		total.Add(total, bal)
		st.balances[addr] = new(big.Int).Set(bal)
	}
	if total.Cmp(MaxTotalSupply) > 0 {
		return nil, ErrGenesisTooLarge
	}
	k := &Kernel{st: st, blocks: make(map[string]*block), head: genesisID}
	k.blocks[genesisID] = &block{id: genesisID}
	return k, nil
}

// AddBlock validates txs against the historical state at parent and, only if
// all succeed, stores the immutable block. On any failure nothing is kept.
// The current head and its visible state are never changed.
func (k *Kernel) AddBlock(id, parent string, txs []Tx) error {
	if _, exists := k.blocks[id]; exists {
		return ErrDuplicateBlock
	}
	pb, ok := k.blocks[parent]
	if !ok {
		return ErrUnknownParent
	}
	if len(txs) > MaxTxsPerBlock {
		return ErrTooManyTxs
	}
	savedHead := k.head
	k.switchTo(parent)
	var log []change
	for _, tx := range txs {
		if err := applyTx(k.st, tx, &log); err != nil {
			k.st.revertAll(log)
			k.switchTo(savedHead)
			return err
		}
	}
	k.blocks[id] = &block{id: id, parent: parent, depth: pb.depth + 1, changes: log}
	// The new block's changes are already live in the state; adopt the
	// block as head so switchTo undoes its journal on the way back.
	k.head = id
	k.switchTo(savedHead)
	return nil
}

// SwitchHead moves the current head to any existing block, ancestor or
// descendant on any branch. Old branches stay available for later switches.
func (k *Kernel) SwitchHead(id string) error {
	if _, ok := k.blocks[id]; !ok {
		return ErrUnknownBlock
	}
	k.switchTo(id)
	return nil
}

// switchTo moves the head by undoing journals up to the LCA and redoing
// journals down to the target. Cost is proportional to the blocks between
// the two heads and the state entries those blocks touched.
func (k *Kernel) switchTo(target string) {
	a := k.blocks[k.head]
	b := k.blocks[target]
	// Lift the deeper cursor; undone blocks are applied in reverse below.
	var undo, redo []*block
	for a.depth > b.depth {
		undo = append(undo, a)
		a = k.blocks[a.parent]
	}
	for b.depth > a.depth {
		redo = append(redo, b)
		b = k.blocks[b.parent]
	}
	for a != b {
		undo = append(undo, a)
		redo = append(redo, b)
		a = k.blocks[a.parent]
		b = k.blocks[b.parent]
	}
	for _, blk := range undo {
		k.st.revertAll(blk.changes)
		k.stats.BlocksProcessed++
		k.stats.StateEntriesTouched += len(blk.changes)
	}
	for i := len(redo) - 1; i >= 0; i-- {
		blk := redo[i]
		for _, c := range blk.changes {
			k.st.apply(c)
		}
		k.stats.BlocksProcessed++
		k.stats.StateEntriesTouched += len(blk.changes)
	}
	k.head = target
}

// Read returns the current head and the requested balances, nonces, grants.
// Entries never seen return zero.
func (k *Kernel) Read(accounts []string, grants []Grant) View {
	v := View{
		Head:     k.head,
		Balances: make(map[string]*big.Int, len(accounts)),
		Nonces:   make(map[string]uint64, len(accounts)),
		Grants:   make(map[Grant]*big.Int, len(grants)),
	}
	for _, addr := range accounts {
		v.Balances[addr] = new(big.Int).Set(k.st.balance(addr))
		v.Nonces[addr] = k.st.nonce(addr)
	}
	for _, g := range grants {
		v.Grants[g] = new(big.Int).Set(k.st.grant(g))
	}
	return v
}

// Stats returns work counters since the last ResetStats.
func (k *Kernel) Stats() Stats { return k.stats }

// ResetStats zeroes the work counters.
func (k *Kernel) ResetStats() { k.stats = Stats{} }
