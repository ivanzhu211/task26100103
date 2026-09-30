package forkchain

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrNegativeAmount   = errors.New("forkchain: negative amount")
	ErrNonPositiveOut   = errors.New("forkchain: non-positive output amount")
	ErrBadNonce         = errors.New("forkchain: nonce mismatch")
	ErrBadOutputCount   = errors.New("forkchain: batch output count out of range")
	ErrInsufficientBal  = errors.New("forkchain: insufficient balance")
	ErrInsufficientGrnt = errors.New("forkchain: insufficient allowance")
)

// applyTx validates and executes one transaction against s, journaling every
// mutation into log. On error the journal entries appended so far are left
// in log; the caller rolls them back with revertAll.
func applyTx(s *state, tx Tx, log *[]change) error {
	switch t := tx.(type) {
	case Approve:
		return applyApprove(s, t, log)
	case Batch:
		return applyBatch(s, t, log)
	default:
		return fmt.Errorf("forkchain: unknown tx type %T", tx)
	}
}

func checkNonce(s *state, sender string, nonce uint64) error {
	if s.nonce(sender) != nonce {
		return fmt.Errorf("%w: sender %q has %d, tx has %d",
			ErrBadNonce, sender, s.nonce(sender), nonce)
	}
	return nil
}

func applyApprove(s *state, t Approve, log *[]change) error {
	if t.Amount == nil || t.Amount.Sign() < 0 {
		return ErrNegativeAmount
	}
	if err := checkNonce(s, t.Sender, t.Nonce); err != nil {
		return err
	}
	s.setGrant(Grant{Owner: t.Sender, Spender: t.Spender}, new(big.Int).Set(t.Amount), log)
	s.setNonce(t.Sender, t.Nonce+1, log)
	return nil
}

func applyBatch(s *state, t Batch, log *[]change) error {
	if len(t.Outputs) == 0 || len(t.Outputs) > MaxOutputsPerBatch {
		return ErrBadOutputCount
	}
	total := new(big.Int)
	for _, o := range t.Outputs {
		if o.Amount == nil || o.Amount.Sign() <= 0 {
			return ErrNonPositiveOut
		}
		total.Add(total, o.Amount)
	}
	if err := checkNonce(s, t.Sender, t.Nonce); err != nil {
		return err
	}
	ownerBal := s.balance(t.Owner)
	if ownerBal.Cmp(total) < 0 {
		return fmt.Errorf("%w: owner %q has %s, needs %s",
			ErrInsufficientBal, t.Owner, ownerBal, total)
	}
	// No exemption when Sender == Owner: the allowance is always required.
	g := Grant{Owner: t.Owner, Spender: t.Sender}
	allowance := s.grant(g)
	if allowance.Cmp(total) < 0 {
		return fmt.Errorf("%w: grant %q->%q has %s, needs %s",
			ErrInsufficientGrnt, t.Owner, t.Sender, allowance, total)
	}
	s.setBalance(t.Owner, new(big.Int).Sub(ownerBal, total), log)
	s.setGrant(g, new(big.Int).Sub(allowance, total), log)
	for _, o := range t.Outputs {
		s.setBalance(o.To, new(big.Int).Add(s.balance(o.To), o.Amount), log)
	}
	s.setNonce(t.Sender, t.Nonce+1, log)
	return nil
}
