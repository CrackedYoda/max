package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	_ = godotenv.Load()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	// golang-migrate selects its driver by scheme.
	url = "pgx5://" + strings.TrimPrefix(strings.TrimPrefix(url, "postgres://"), "postgresql://")

	m, err := migrate.New("file://db/migrations", url)
	if err != nil {
		return err
	}
	defer m.Close()

	cmd := "up"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "up":
		err = m.Up()
	case "down":
		n := 1
		if len(args) > 1 {
			if n, err = strconv.Atoi(args[1]); err != nil {
				return fmt.Errorf("invalid step count: %w", err)
			}
		}
		err = m.Steps(-n)
	case "version":
		v, dirty, verr := m.Version()
		if verr != nil {
			return verr
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
		return nil
	default:
		return fmt.Errorf("unknown command %q (use up | down [n] | version)", cmd)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no change")
		return nil
	}
	if err == nil {
		fmt.Println("ok")
	}
	return err
}
