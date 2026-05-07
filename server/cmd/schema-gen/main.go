// Command schema-gen reflects the server-config Go structs into JSON
// Schema files committed under libs/proto-ts/schema/. Desktop + phone
// config UIs can use these to validate user edits without re-
// implementing the struct layout.
//
// Usage: `make schema-gen` (runs `go run ./cmd/schema-gen` from the
// server module). Output paths are deterministic — re-running
// overwrites in place so the commit diff reviews cleanly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

	"github.com/notbbg/notbbg/server/internal/config"
)

func main() {
	outDir := flag.String("out", "libs/proto-ts/schema",
		"output directory for generated .json schemas")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}

	// The server config is written in YAML, not JSON, so tell the
	// reflector to read the `yaml` struct tag when naming fields.
	// Also mark fields that don't carry `omitempty` as required, so
	// the schema faithfully reflects which fields the loader expects.
	reflector := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: false,
		AllowAdditionalProperties:  true,
		ExpandedStruct:             false,
		DoNotReference:             false,
	}

	targets := []struct {
		name string
		v    any
	}{
		{"config", &config.Config{}},
		{"feeds_config", &config.FeedsConfig{}},
		{"server_config", &config.ServerConfig{}},
		{"datalake_config", &config.DatalakeConfig{}},
		{"cache_config", &config.CacheConfig{}},
		{"alerts_config", &config.AlertsConfig{}},
		{"exchange_config", &config.ExchangeConfig{}},
		{"feed_source_config", &config.FeedSourceConfig{}},
		{"file_feed_config", &config.FileFeedConfig{}},
	}

	for _, t := range targets {
		schema := reflector.Reflect(t.v)
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			log.Fatalf("marshal %s: %v", t.name, err)
		}
		data = append(data, '\n')
		path := filepath.Join(*outDir, t.name+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(data))
	}
}
