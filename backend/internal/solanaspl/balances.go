package solanaspl

import (
	"errors"
	"strconv"
)

type TokenAmount struct {
	Amount   string
	Decimals int
}
type TokenBalance struct {
	AccountIndex  *int
	Mint          string
	Owner         string
	ProgramID     string
	UITokenAmount TokenAmount
}

func (t Transfer) CheckTokenDeltas(source, dest int, pre, post []TokenBalance) error {
	balances := func(rows []TokenBalance, allowMissingDest bool) (uint64, uint64, error) {
		amounts := map[int]uint64{}
		for _, b := range rows {
			if b.AccountIndex == nil {
				return 0, 0, errors.New("missing token balance index")
			}
			i := *b.AccountIndex
			owner := t.Sender
			if i == dest {
				owner = t.Recipient
			}
			_, duplicate := amounts[i]
			if duplicate || (i != dest && i != source) || b.Owner != owner || b.Mint != t.Mint || b.ProgramID != Program || b.UITokenAmount.Decimals != Decimals {
				return 0, 0, errors.New("token balance identity differs")
			}
			n, err := strconv.ParseUint(b.UITokenAmount.Amount, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != b.UITokenAmount.Amount {
				return 0, 0, errors.New("invalid token balance")
			}
			amounts[i] = n
		}
		_, haveSource := amounts[source]
		_, haveDest := amounts[dest]
		if !haveSource || (!allowMissingDest && !haveDest) {
			return 0, 0, errors.New("missing SPL token balance")
		}
		return amounts[source], amounts[dest], nil
	}
	s0, d0, err := balances(pre, t.CreateATA)
	if err != nil {
		return err
	}
	s1, d1, err := balances(post, false)
	if err != nil {
		return err
	}
	if s0 < s1 || d1 < d0 || s0-s1 != t.Amount || d1-d0 != t.Amount {
		return errors.New("SPL token deltas differ from transfer")
	}
	return nil
}
