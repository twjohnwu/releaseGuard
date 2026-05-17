package main

import (
	"context"
	"fmt"
	"os"
)

type Subcommand func(ctx context.Context, args []string) error

var subcommands = map[string]Subcommand{
	"backfill": Backfill,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || os.Args[1] == "--help" {
		fmt.Println("Usage: indexer <subcommand> [flags]")
		fmt.Println("Subcommands:")
		for n := range subcommands {
			fmt.Println("  ", n)
		}
		if len(os.Args) < 2 {
			return fmt.Errorf("no subcommand")
		}
		return nil
	}
	name := os.Args[1]
	fn, ok := subcommands[name]
	if !ok {
		return fmt.Errorf("unknown subcommand: %s", name)
	}
	return fn(context.Background(), os.Args[2:])
}

// Backfill stub — implemented in Plan D
func Backfill(ctx context.Context, args []string) error {
	return fmt.Errorf("backfill not implemented in Plan C")
}
