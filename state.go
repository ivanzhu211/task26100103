package forkchain

import "math/big"

// balChg records one account's balance before and after a block.
// A nil old means the account had no balance entry before the block.
type balChg struct {
	old *big.Int
	new *big.Int
}

// nonceChg records one account's nonce before and after a block.
type nonceChg struct {
	old uint64
	had bool
	new uint64
}

// allowChg records one grant's allowance before and after a block.
// A nil old means the grant had no allowance entry before the block.
type allowChg struct {
	old *big.Int
	new *big.Int
}

// delta is the exact set of state entries one block changed. It supports
// O(touched entries) apply and undo, with no full-state copying.
type delta struct {
	bal   map[string]balChg
	nonce map[string]nonceChg
	allow map[Grant]allowChg
}

func newDelta() *delta {
	return &delta{
		bal:   make(map[string]balChg),
		nonce: make(map[string]nonceChg),
		allow: make(map[Grant]allowChg),
	}
}

func (c *Chain) balanceOf(addr string) *big.Int {
	if v, ok := c.balance[addr]; ok {
		return v
	}
	return new(big.Int)
}

func (c *Chain) nonceOf(addr string) uint64 {
	return c.nonce[addr]
}

func (c *Chain) allowanceOf(owner, spender string) *big.Int {
	if v, ok := c.allowance[Grant{Owner: owner, Spender: spender}]; ok {
		return v
	}
	return new(big.Int)
}

// setBalance records the pre-block value on first touch and installs v.
// big.Int values are never mutated in place, so state and deltas may share
// pointers safely.
func (c *Chain) setBalance(d *delta, addr string, v *big.Int) {
	if _, ok := d.bal[addr]; !ok {
		var old *big.Int
		if cur, had := c.balance[addr]; had {
			old = cur
		}
		d.bal[addr] = balChg{old: old}
	}
	c.balance[addr] = v
	c.stats.EntriesTouched++
}

func (c *Chain) setNonce(d *delta, addr string, v uint64) {
	if _, ok := d.nonce[addr]; !ok {
		cur, had := c.nonce[addr]
		d.nonce[addr] = nonceChg{old: cur, had: had}
	}
	c.nonce[addr] = v
	c.stats.EntriesTouched++
}

func (c *Chain) setAllowance(d *delta, owner, spender string, v *big.Int) {
	g := Grant{Owner: owner, Spender: spender}
	if _, ok := d.allow[g]; !ok {
		var old *big.Int
		if cur, had := c.allowance[g]; had {
			old = cur
		}
		d.allow[g] = allowChg{old: old}
	}
	c.allowance[g] = v
	c.stats.EntriesTouched++
}

// finalize snapshots the post-block values of every touched entry so the
// delta can be re-applied later when its branch is activated again.
func (c *Chain) finalize(d *delta) {
	for addr, chg := range d.bal {
		chg.new = c.balance[addr]
		d.bal[addr] = chg
	}
	for addr, chg := range d.nonce {
		chg.new = c.nonce[addr]
		d.nonce[addr] = chg
	}
	for g, chg := range d.allow {
		chg.new = c.allowance[g]
		d.allow[g] = chg
	}
}

// applyDelta moves the visible state forward across one stored block.
func (c *Chain) applyDelta(d *delta) {
	for addr, chg := range d.bal {
		c.balance[addr] = chg.new
	}
	for addr, chg := range d.nonce {
		c.nonce[addr] = chg.new
	}
	for g, chg := range d.allow {
		c.allowance[g] = chg.new
	}
	c.stats.EntriesTouched += int64(len(d.bal) + len(d.nonce) + len(d.allow))
}

// undoDelta restores the pre-block values, deleting entries that did not
// exist before the block. Works for both finalized and partial deltas.
func (c *Chain) undoDelta(d *delta) {
	for addr, chg := range d.bal {
		if chg.old == nil {
			delete(c.balance, addr)
		} else {
			c.balance[addr] = chg.old
		}
	}
	for addr, chg := range d.nonce {
		if !chg.had {
			delete(c.nonce, addr)
		} else {
			c.nonce[addr] = chg.old
		}
	}
	for g, chg := range d.allow {
		if chg.old == nil {
			delete(c.allowance, g)
		} else {
			c.allowance[g] = chg.old
		}
	}
	c.stats.EntriesTouched += int64(len(d.bal) + len(d.nonce) + len(d.allow))
}
