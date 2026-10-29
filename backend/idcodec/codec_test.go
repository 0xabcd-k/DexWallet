package idcodec

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/capitalone/fpe/ff1"
)

const testKey = "test-only-key123"

func TestCodecRoundTripAndDexpokerFormat(t *testing.T) {
	c, err := New(testKey, User)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 42, 9007199254740993, 10000000000000000, math.MaxInt64} {
		got, err := c.Encode(id)
		if err != nil {
			t.Fatal(err)
		}
		// Reference formula from dexpoker, with a public test-only key.
		reference, err := ff1.NewCipher(10, len(User), []byte(testKey), []byte(User))
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := reference.Encrypt(fmt.Sprintf("%016d", id))
		if err != nil {
			t.Fatal(err)
		}
		if got != "0"+encrypted {
			t.Fatalf("format mismatch: %s", got)
		}
		decoded, err := c.Decode(got)
		if err != nil || decoded != id {
			t.Fatalf("roundtrip: %d %v", decoded, err)
		}
		repeated, _ := c.Encode(id)
		if repeated != got {
			t.Fatal("unstable idx")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := int64(1); j < 40; j++ {
				v, e := c.Encode(j)
				if e != nil {
					t.Error(e)
					return
				}
				id, e := c.Decode(v)
				if e != nil || id != j {
					t.Errorf("concurrent roundtrip: %d %v", id, e)
				}
			}
		}()
	}
	wg.Wait()
}
func TestInvalidIDsAndConfiguration(t *testing.T) {
	for _, key := range []string{"", "short"} {
		if _, err := New(key, User); err == nil {
			t.Fatal("invalid key")
		}
	}
	if _, err := New(testKey, ""); err == nil {
		t.Fatal("empty kind")
	}
	c, _ := New(testKey, User)
	for _, id := range []int64{0, -1, math.MinInt64} {
		if _, err := c.Encode(id); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	invalid := []string{"", "0", "10000000000000000", "0000000000000000x", "000000000000000000000", "0+123456789012345"}
	for _, plain := range []string{"0000000000000000", "9223372036854775808", "00000000000000001"} {
		ref, _ := ff1.NewCipher(10, len(User), []byte(testKey), []byte(User))
		encrypted, err := ref.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		invalid = append(invalid, "0"+encrypted)
	}
	for _, idx := range invalid {
		if _, err := c.Decode(idx); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %q: %v", idx, err)
		}
	}
}
