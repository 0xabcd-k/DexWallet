package chainassets

import "testing"

func TestTONRelayStorageThroughSignedDeadline(t *testing.T) {
	zero, one, cells, bits, cellPrice := int64(0), int64(1), int64(22), int64(5012), int64(135)
	a := &tonStorageUsed{Cells: &cells, Bits: &bits}
	current := tonStoragePrice{Since: &zero, Bit: &zero, Cell: &cellPrice}
	cost, err := tonRelayStorageCost([]tonStoragePrice{current}, a, a, 100, 400)
	if err != nil || cost.Shift(9).IntPart() != 29 {
		t.Fatal("observed W5 rent and separate rounding", cost, err)
	}
	later := int64(200)
	updated := tonStoragePrice{Since: &later, Bit: &one, Cell: &cellPrice}
	higher, err := tonRelayStorageCost([]tonStoragePrice{current, updated}, a, a, 100, 400)
	if err != nil || !higher.GreaterThan(cost) {
		t.Fatal("announced price change", higher, err)
	}
	if _, err = tonRelayStorageCost([]tonStoragePrice{updated}, a, a, 100, 400); err == nil {
		t.Fatal("missing current price")
	}
	if _, err = tonRelayStorageCost([]tonStoragePrice{current}, nil, a, 100, 400); err == nil {
		t.Fatal("unknown storage size")
	}
}
