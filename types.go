package forkchain

import "math/big"

// Output is a single payment entry of a Batch transaction.
type Output struct {
	To     string
	Amount *big.Int
}

// TxKind discriminates the two supported transaction types.
type TxKind int

const (
	TxApprove TxKind = iota
	TxBatch
)

// Tx is a single transaction. Use Approve or Batch to construct one.
type Tx struct {
	Kind    TxKind
	Sender  string
	Spender string
	Owner   string
	Amount  *big.Int
	Outputs []Output
	Nonce   uint64
}

// Approve builds a transaction replacing sender's allowance for spender.
func Approve(sender, spender string, amount *big.Int, nonce uint64) Tx {
	return Tx{Kind: TxApprove, Sender: sender, Spender: spender, Amount: amount, Nonce: nonce}
}

// Batch builds a transaction paying outputs from owner, authorized by sender.
func Batch(sender, owner string, outputs []Output, nonce uint64) Tx {
	return Tx{Kind: TxBatch, Sender: sender, Owner: owner, Outputs: outputs, Nonce: nonce}
}

// Grant identifies an (owner, spender) allowance pair.
type Grant struct {
	Owner   string
	Spender string
}

// AccountView is the externally visible state of one account.
type AccountView struct {
	Balance *big.Int
	Nonce   uint64
}

// Stats counts actually processed blocks and touched state entries,
// used to prove switches do not scan the common prefix or all accounts.
type Stats struct {
	BlocksProcessed int64
	EntriesTouched  int64
}
