package forkchain

import (
	"fmt"
	"math/big"
)

const maxOutputsPerBatch = 500

// execTx validates and applies one transaction against the current in-memory
// state, recording every touched entry into d. On error the caller undoes d.
func (c *Chain) execTx(d *delta, tx Tx) error {
	switch tx.Kind {
	case TxApprove:
		return c.execApprove(d, tx)
	case TxBatch:
		return c.execBatch(d, tx)
	default:
		return fmt.Errorf("%w: unknown kind", ErrInvalidTx)
	}
}

func (c *Chain) execApprove(d *delta, tx Tx) error {
	if tx.Amount == nil || tx.Amount.Sign() < 0 {
		return fmt.Errorf("%w: approve amount must be non-negative", ErrInvalidTx)
	}
	if c.nonceOf(tx.Sender) != tx.Nonce {
		return fmt.Errorf("%w: sender %s", ErrBadNonce, tx.Sender)
	}
	c.setAllowance(d, tx.Sender, tx.Spender, new(big.Int).Set(tx.Amount))
	c.setNonce(d, tx.Sender, tx.Nonce+1)
	return nil
}

func (c *Chain) execBatch(d *delta, tx Tx) error {
	if len(tx.Outputs) == 0 || len(tx.Outputs) > maxOutputsPerBatch {
		return fmt.Errorf("%w: batch needs 1..500 outputs", ErrInvalidTx)
	}
	if c.nonceOf(tx.Sender) != tx.Nonce {
		return fmt.Errorf("%w: sender %s", ErrBadNonce, tx.Sender)
	}
	total := new(big.Int)
	for _, o := range tx.Outputs {
		if o.Amount == nil || o.Amount.Sign() <= 0 {
			return fmt.Errorf("%w: output amount must be positive", ErrInvalidTx)
		}
		total.Add(total, o.Amount)
	}
	if c.balanceOf(tx.Owner).Cmp(total) < 0 {
		return fmt.Errorf("%w: owner %s", ErrInsufficientBalance, tx.Owner)
	}
	// The allowance check applies even when sender == owner.
	if c.allowanceOf(tx.Owner, tx.Sender).Cmp(total) < 0 {
		return fmt.Errorf("%w: %s -> %s", ErrInsufficientAllowance, tx.Owner, tx.Sender)
	}
	c.setBalance(d, tx.Owner, new(big.Int).Sub(c.balanceOf(tx.Owner), total))
	c.setAllowance(d, tx.Owner, tx.Sender, new(big.Int).Sub(c.allowanceOf(tx.Owner, tx.Sender), total))
	for _, o := range tx.Outputs {
		c.setBalance(d, o.To, new(big.Int).Add(c.balanceOf(o.To), o.Amount))
	}
	c.setNonce(d, tx.Sender, tx.Nonce+1)
	return nil
}
