// Package testenv is tooling-only. It never discovers credentials or accepts production targets.
package testenv

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
)

const AdminUser = "dexwallet_test_admin"
const HTTPUser = "dexwallet_test_http"
const HTTPDatabase = "dexwallet_test_http"
const DatabasePrefix = "dexwallet_contract_"
const RedisAddress = "127.0.0.1:6379"
const NameServer = "127.0.0.1:9876"
const BrokerAddress = "127.0.0.1:10911"

var databaseName = regexp.MustCompile(`^dexwallet_contract_[a-z0-9_]+$`)

// ValidateDSN is deliberately local-only for phase 1.5. A name/flag is not a
// permission boundary: provisioning must grant these accounts only test schemas.
func ValidateDSN(secret, role string) (*mysql.Config, error) {
	if os.Getenv("DEXWALLET_TEST_ENV") != "local" {
		return nil, errors.New("DEXWALLET_TEST_ENV=local required; production is forbidden")
	}
	allowed := os.Getenv("DEXWALLET_TEST_MYSQL_ADDRESS")
	host, _, err := net.SplitHostPort(allowed)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("explicit loopback DEXWALLET_TEST_MYSQL_ADDRESS required")
	}
	c, err := mysql.ParseDSN(secret)
	if err != nil || secret == "" {
		return nil, errors.New("dedicated test DSN required (value omitted)")
	}
	if c.Net != "tcp" || c.Addr != allowed {
		return nil, errors.New("test DSN endpoint is not the allowed local target")
	}
	switch role {
	case "admin":
		if c.User != AdminUser || c.DBName != "" {
			return nil, errors.New("test preparation requires dexwallet_test_admin with no default schema")
		}
	case "http":
		if c.User != HTTPUser || c.DBName != HTTPDatabase || c.MultiStatements {
			return nil, errors.New("HTTP tests require the scoped dexwallet_test_http account/schema")
		}
	case "migration":
		if c.User != AdminUser || !databaseName.MatchString(c.DBName) {
			return nil, errors.New("migration requires a dedicated contract schema and test account")
		}
	default:
		return nil, errors.New("unknown test connection role")
	}
	if c.Passwd == "" || c.AllowAllFiles || c.AllowCleartextPasswords || c.AllowFallbackToPlaintext || !c.AllowNativePasswords {
		return nil, errors.New("unsafe or incomplete test credentials")
	}
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second
	return c, nil
}

func AdminConfig() (*mysql.Config, error) {
	return ValidateDSN(os.Getenv("DEXWALLET_TEST_MYSQL_DSN"), "admin")
}
func CheckLocalDependencies(redis string, names []string, broker string) error {
	if os.Getenv("DEXWALLET_TEST_ENV") != "local" || redis != RedisAddress || len(names) != 1 || names[0] != NameServer || broker != BrokerAddress {
		return errors.New("integration dependencies must use the documented local test endpoints")
	}
	return nil
}

func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "tools.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run inside the DexWallet checkout")
		}
		dir = parent
	}
}
