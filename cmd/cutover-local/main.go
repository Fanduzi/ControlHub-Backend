// Package main provides the local bigint cutover CLI entry point.
// input: config.LoadDotEnv/Load/ErrQueryExecutionTokenMaxAgeRejected, cutover.RunLocalPreserveThenImport, flag
// output: local preserve-then-import cutover binary
// pos: Operator-invoked CLI that preserves legacy UUID tables and rebuilds with bigint schema
// note: if cutover logic or config loading changes, update this header and cmd/cutover-local/README.md
package main

import (
	"context"
	"flag"
	"log"

	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/cutover"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	preserveDB := flag.String("preserve-db", "", "database name used to preserve the legacy UUID-backed runtime tables (default: <target-db>_v1)")
	targetDB := flag.String("target-db", "controlhub", "database name to rebuild with bigint schema")
	resume := flag.Bool("resume", false, "continue a previously preserved local cutover using an existing non-empty preserve database")
	flag.Parse()

	effectivePreserveDB := effectivePreserveDBName(*targetDB, *preserveDB)
	if err := cutover.RunLocalPreserveThenImport(context.Background(), cutover.LocalCutoverConfig{
		RuntimeDSN:     cfg.DatabaseDSN,
		PreserveDBName: effectivePreserveDB,
		TargetDBName:   *targetDB,
		Resume:         *resume,
	}); err != nil {
		log.Fatalf("local preserve-then-import cutover failed: %v", err)
	}

	log.Printf("local preserve-then-import cutover completed: target=%s preserved=%s", *targetDB, effectivePreserveDB)
}

func effectivePreserveDBName(targetDB string, preserveDB string) string {
	if preserveDB != "" {
		return preserveDB
	}
	return targetDB + "_v1"
}
