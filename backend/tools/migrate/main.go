package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/golang-migrate/migrate/v4"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute() error {
	target := flag.String("database", "", "explicit database matching DEXWALLET_MYSQL_DSN")
	source := flag.String("source", "", "migration set: examples or business")
	action := flag.String("action", "", "up or version; no automatic rollback/force")
	flag.Parse()
	if (*source != "examples" && *source != "business") || !strings.HasPrefix(*target, "dexwallet_contract_") {
		return errors.New("explicit --source examples|business and --database dexwallet_contract_<name> are required")
	}
	if *action != "up" && *action != "version" {
		return errors.New("--action must be up or version")
	}
	directory := filepath.Join("manifest", "sql", "diff", *source)
	if _, err := os.Stat(directory); err != nil {
		return errors.New("run from backend/; migration directory missing")
	}
	m, err := migration.Open(os.Getenv("DEXWALLET_MYSQL_DSN"), *target, directory, *source)
	if err != nil {
		return err
	}
	defer m.Close()
	if *action == "up" {
		if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
	}
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("version=none dirty=false")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("database=%s version=%d dirty=%t\n", *target, version, dirty)
	return nil
}
