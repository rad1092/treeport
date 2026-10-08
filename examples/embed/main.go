// Embed the deterministic export model before accepting a generated tree.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/rad1092/treeport"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := treeport.Check(ctx, []treeport.Entry{
		{Path: "CAFÉ/a?b.txt", Kind: "file"},
		{Path: "cafe\u0301/a*b.txt", Kind: "file"},
	}, treeport.Options{Profile: "export-fold", DestinationRoot: "/exports", Limits: treeport.DefaultLimits()})
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		log.Fatal(err)
	}
	// A real export service would reject incompatible/unknown/incomplete reports
	// according to its policy before writing anything. This example only reports.
}
