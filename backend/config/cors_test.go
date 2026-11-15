package config

import (
	"strings"
	"testing"
)

func TestCORSOriginsRejectWildcardAndNonOrigins(t *testing.T) {
	for _, origin := range []string{"*", "https://*.example.com", "http://localhost", "https://host/path", "https://host?token=value", "https://user:pass@host", "https://host#fragment"} {
		c := Config{CORSOrigins: []string{origin}}
		err := c.Check()
		if err == nil || !strings.Contains(err.Error(), "CORSOrigins") {
			t.Fatal("accepted", origin)
		}
	}
}
