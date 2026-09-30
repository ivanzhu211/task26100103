package forkchain

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrBlockExists           = errors.New("forkchain: block id already exists")
	ErrUnknownParent         = errors.New("forkchain: unknown parent block")
	ErrUnknownBlock          = errors.New("forkchain: unknown block")
	ErrTooManyTxs            = errors.New("forkchain: block exceeds 200 transactions")
	ErrInvalidTx             = errors.New("forkchain: invalid transaction")
	ErrBadNonce              = errors.New("forkchain: nonce mismatch")
	ErrInsufficientBalance   = errors.New("forkchain: insufficient balance")
	ErrInsufficientAllowance = errors.New("forkchain: insufficient allowance")
	ErrSupplyExceeded        = errors.New("forkchain: genesis supply exceeds 10^27")
)

// maxSupply is the hard cap on total genesis balances: 10^27.
var maxSupply = new(big.Int).Exp(big.NewInt(10), big.NewInt(27), nil)

const maxTxsPerBlock = 200

// block is an immutable node of the chain tree. Its delta holds exactly the
// state entries this block changed, so switching heads only touches the
// blocks between the two heads and their changed entries.
type block struct {
	id     string
	parent string
	txs    []Tx
	depth  int
	delta  *delta
}

// Chain is the fork-state kernel. All calls are serial; state lives in memory.
type Chain struct {
	genesis string
	head    string
	blocks  map[string]*block

	balance   map[string]*big.Int
	nonce     map[string]uint64
	allowance map[Grant]*big.Int

	stats Stats
}

// NewChain creates a chain with a retained genesis block and caller-supplied
// genesis balances. Nonces and allowances start at zero.
func NewChain(genesisID string, balances map[string]*big.Int) (*Chain, error) {
	c := &Chain{
		genesis:   genesisID,
		head:      genesisID,
		blocks:    make(map[string]*block),
		balance:   make(map[string]*big.Int, len(balances)),
		nonce:     make(map[string]uint64),
		allowance: make(map[Grant]*big.Int),
	}
	total := new(big.Int)
	for addr, amt := range balances {
		if amt == nil || amt.Sign() < 0 {
			return nil, fmt.Errorf("%w: genesis balance of %s", ErrInvalidTx, addr)
		}
		if amt.Sign() > 0 {
			c.balance[addr] = new(big.Int).Set(amt)
			total.Add(total, amt)
		}
	}
	if total.Cmp(maxSupply) > 0 {
		return nil, ErrSupplyExceeded
	}
	c.blocks[genesisID] = &block{id: genesisID, delta: newDelta()}
	return c, nil
}

// AddBlock validates txs against the historical state at parent and, only if
// all succeed, stores an immutable block. The current head is never changed.
func (c *Chain) AddBlock(id, parent string, txs []Tx) error {
	if _, exists := c.blocks[id]; exists {
		return fmt.Errorf("%w: %s", ErrBlockExists, id)
	}
	p, ok := c.blocks[parent]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownParent, parent)
	}
	if len(txs) > maxTxsPerBlock {
		return fmt.Errorf("%w: %d", ErrTooManyTxs, len(txs))
	}

	saved := c.head
	// Validate against the parent's historical state. Cost is proportional to
	// the path between head and parent, never to the whole chain.
	c.switchHead(parent)

	d := newDelta()
	var err error
	for i, tx := range txs {
		if err = c.execTx(d, tx); err != nil {
			err = fmt.Errorf("tx %d: %w", i, err)
			break
		}
	}
	if err != nil {
		// Roll back the partial effects; nothing of this block survives.
		c.undoDelta(d)
		c.switchHead(saved)
		return err
	}
	c.finalize(d)
	c.blocks[id] = &block{id: id, parent: parent, txs: txs, depth: p.depth + 1, delta: d}
	// The new block's effects are not part of any active path yet; roll them
	// back before returning to the saved head.
	c.undoDelta(d)
	c.switchHead(saved)
	return nil
}

// SwitchHead moves the visible state to any existing block, keeping old
// branches available for later reactivation.
func (c *Chain) SwitchHead(id string) error {
	if _, ok := c.blocks[id]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownBlock, id)
	}
	c.switchHead(id)
	return nil
}

// switchHead undoes deltas from the current head up to the lowest common
// ancestor with id, then re-applies deltas down to id. Work is bounded by
// the blocks between the two heads and the entries those blocks touched.
func (c *Chain) switchHead(id string) {
	if c.head == id {
		return
	}
	a, b := c.blocks[c.head], c.blocks[id]
	var undo, apply []*block
	// Climb the deeper side until both are at the same depth.
	for a.depth > b.depth {
		undo = append(undo, a)
		a = c.blocks[a.parent]
	}
	for b.depth > a.depth {
		apply = append(apply, b)
		b = c.blocks[b.parent]
	}
	// Climb both sides together until they meet at the LCA.
	for a != b {
		undo = append(undo, a)
		a = c.blocks[a.parent]
		apply = append(apply, b)
		b = c.blocks[b.parent]
	}
	for _, blk := range undo {
		c.undoDelta(blk.delta)
	}
	// apply was collected from the target upwards; replay it LCA-first so a
	// child's value always wins over its parent's for the same entry.
	for i := len(apply) - 1; i >= 0; i-- {
		c.applyDelta(apply[i].delta)
	}
	c.stats.BlocksProcessed += int64(len(undo) + len(apply))
	c.head = id
}

// Head returns the current head block id.
func (c *Chain) Head() string {
	return c.head
}

// HasBlock reports whether id names a stored block.
func (c *Chain) HasBlock(id string) bool {
	_, ok := c.blocks[id]
	return ok
}

// Read returns the current head, the balance and nonce of each requested
// account, and the allowance of each requested grant. Unseen entries are zero.
func (c *Chain) Read(accounts []string, grants []Grant) (string, map[string]AccountView, map[Grant]*big.Int) {
	views := make(map[string]AccountView, len(accounts))
	for _, addr := range accounts {
		views[addr] = AccountView{
			Balance: new(big.Int).Set(c.balanceOf(addr)),
			Nonce:   c.nonceOf(addr),
		}
	}
	allows := make(map[Grant]*big.Int, len(grants))
	for _, g := range grants {
		allows[g] = new(big.Int).Set(c.allowanceOf(g.Owner, g.Spender))
	}
	return c.head, views, allows
}

// ResetStats zeroes the processed-block / touched-entry counters.
func (c *Chain) ResetStats() {
	c.stats = Stats{}
}

// Stats returns the counters accumulated since the last ResetStats.
func (c *Chain) Stats() Stats {
	return c.stats
}
