// eval is a local-only tool for comparing LLM digest quality/cost across
// models on an identical input (ADR-21, milestone M26-lite). It never posts
// to Telegram. It has two subcommands:
//
//   - capture: connects to Telegram using the same session/config as the
//     svodka job, fetches the configured source chats within the window, and
//     freezes them into a snapshot file under eval/, so later `run`
//     invocations need no Telegram secrets and see byte-identical input.
//   - run: reads a snapshot and runs the production digest.Build against one
//     or more candidate models via OpenRouter, writing each model's digest
//     plus a metrics summary.
//
// There is no judge phase: ranking candidates is a manual review step outside
// this tool (out of scope per ADR-21).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"svodka/config"
	"svodka/foundation/logger"
)

func main() {
	log := logger.New("eval")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "capture", "run":
	default:
		usage()
		os.Exit(2)
	}

	// ardanlabs/conf's Parse (called deep inside config.LoadForCapture) and
	// stdlib flag (used directly by runRun) both read the process-global
	// os.Args rather than an argv we could pass in, so the subcommand token
	// must be stripped here, before either runs — this makes `eval capture
	// --window-hours 2` look like the flat "--window-hours 2" both parsers
	// expect.
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	var err error
	switch sub {
	case "capture":
		err = runCapture(ctx, log)
	case "run":
		err = runRun(ctx, log)
	}

	if err != nil {
		if errors.Is(err, config.ErrHelp) {
			return
		}
		log.Error(ctx, "eval failed", "err", err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: eval <capture|run> [flags]")
	fmt.Fprintln(os.Stderr, "  eval capture --config config.local.yml --window-hours 24")
	fmt.Fprintln(os.Stderr, "  eval run --snapshot eval/capture-<ts>.json --models \"vendor/model,...\"")
}
