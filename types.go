// Package forkchain implements a local blockchain fork-state kernel.
//
// The kernel keeps a single in-memory mutable state for the current head
// plus per-block change journals, so switching heads only costs the blocks
// between the two heads and the state entries those blocks touched.
package forkchain

import "math/big"

// MaxTotalSupply bounds the genesis total (10^27 smallest units).
var MaxTotalSupply = new(big.Int).Exp(big.NewInt(10), big.NewInt(27), nil)

// MaxTxsPerBlock is the maximum number of transactions per block.
const MaxTxsPerBlock = 200

// MaxOutputsPerBatch is the maximum number of outputs in a Batch tx.
const MaxOutputsPerBatch = 500

// Output is a single payment destination of a Batch transaction.
type Output struct {
	To     string
	Amount *big.Int
}

// Tx is the sealed transaction interface (Approve or Batch).
type Tx interface{ isTx() }

// Approve replaces sender's allowance for spender with Amount.
type Approve struct {
	Sender  string
	Spender string
	Amount  *big.Int
	Nonce   uint64
}

func (Approve) isTx() {}

// Batch pays Outputs from Owner's balance, authorized by Owner->Sender grant.
type Batch struct {
	Sender  string
	Owner   string
	Outputs []Output
	Nonce   uint64
}

func (Batch) isTx() {}

// Grant identifies an owner->spender allowance.
type Grant struct {
	Owner   string
	Spender string
}

// View is the result of Read: the current head plus requested entries.
type View struct {
	Head     string
	Balances map[string]*big.Int
	Nonces   map[string]uint64
	Grants   map[Grant]*big.Int
}

// Stats counts work performed since the last ResetStats call.
type Stats struct {
	BlocksProcessed     int
	StateEntriesTouched int
}
