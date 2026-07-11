package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"svodka/business/digest"
	"svodka/business/llm"
	"svodka/foundation/logger"
)

// modelTimeout bounds one candidate model's whole digest.Build call (a
// single pass, or N maps + reduce), mirroring app/summarize's llmTimeout.
const modelTimeout = 5 * time.Minute

// runRun loads a snapshot produced by `eval capture` and runs the production
// digest.Build against each requested model via OpenRouter, writing a
// per-model digest file plus a metrics summary. It never calls config.Load or
// config.LoadForCapture and never touches Telegram — the snapshot alone is
// enough input (ADR-21).
func runRun(ctx context.Context, log *logger.Logger) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	snapshotPath := fs.String("snapshot", "", "path to a snapshot produced by `eval capture` (required)")
	modelsCSV := fs.String("models", "", "comma-separated OpenRouter model ids to compare (required)")
	maxOutputTokensFlag := fs.Int("max-output-tokens", 0, "override the snapshot's max_output_tokens (0 = use the snapshot's value)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	if *snapshotPath == "" {
		return errors.New("--snapshot is required")
	}
	if *modelsCSV == "" {
		return errors.New("--models is required")
	}
	models := splitModels(*modelsCSV)
	if len(models) == 0 {
		return errors.New("--models did not contain any model ids")
	}

	key := os.Getenv("SVODKA_OPENROUTER_KEY")
	if key == "" {
		return errors.New("SVODKA_OPENROUTER_KEY is not set; eval run calls every candidate through OpenRouter")
	}

	data, err := os.ReadFile(*snapshotPath)
	if err != nil {
		return fmt.Errorf("read snapshot %q: %w", *snapshotPath, err)
	}
	snap, err := decodeSnapshot(data)
	if err != nil {
		return fmt.Errorf("decode snapshot %q: %w", *snapshotPath, err)
	}

	loc, err := time.LoadLocation(snap.Settings.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", snap.Settings.Timezone, err)
	}

	maxTokens := snap.Settings.MaxOutputTokens
	if *maxOutputTokensFlag > 0 {
		maxTokens = *maxOutputTokensFlag
	}

	dir := filepath.Join("eval", "run-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}

	results := make([]modelResult, 0, len(models))
	for _, model := range models {
		results = append(results, runModel(ctx, log, key, dir, model, snap, loc, maxTokens))
	}

	succeeded := 0
	for _, r := range results {
		if r.Err == nil {
			succeeded++
		}
	}
	if succeeded == 0 {
		return errors.New("all models failed; see warnings above")
	}

	summary := formatSummary(results)
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(summary), 0o644); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	fmt.Println(summary)
	return nil
}

// runModel runs one candidate model's digest.Build and writes its digest file
// on success. Errors are logged and reflected in the returned modelResult,
// never returned to the caller, so one bad model does not abort the others.
func runModel(ctx context.Context, log *logger.Logger, key, dir, model string, snap Snapshot, loc *time.Location, maxTokens int) modelResult {
	provider := llm.NewOpenRouter(key)

	llmCtx, cancel := context.WithTimeout(ctx, modelTimeout)
	defer cancel()

	start := time.Now()
	text, usage, err := digest.Build(llmCtx, provider, snap.Chats, digest.Options{
		OutputLang:        snap.Settings.OutputLang,
		Location:          loc,
		Model:             model,
		MaxTokens:         maxTokens,
		ExtraInstructions: snap.Settings.ExtraInstructions,
		Backlinks:         snap.Settings.Backlinks,
	})
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		// The model id itself is not content/secret (NFR-2 protects message
		// content), so naming it in this one warning is fine and useful.
		log.Warn(ctx, "model failed", "model", model, "err", err.Error())
		return modelResult{Model: model, Err: err}
	}

	digestChars := len([]rune(text))
	path := filepath.Join(dir, "digest-"+sanitizeModel(model)+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		log.Warn(ctx, "writing digest failed", "model", model, "err", err.Error())
		return modelResult{Model: model, Err: err}
	}

	log.Info(ctx, "model done",
		"model", model,
		"tokens_in", usage.InputTokens, "tokens_out", usage.OutputTokens,
		"duration_ms", durationMs, "digest_chars", digestChars, "truncated", usage.Truncated)

	return modelResult{
		Model:       model,
		TokensIn:    usage.InputTokens,
		TokensOut:   usage.OutputTokens,
		DurationMs:  durationMs,
		DigestChars: digestChars,
		Truncated:   usage.Truncated,
	}
}
