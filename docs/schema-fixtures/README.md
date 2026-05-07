# schema-fixtures

Golden protojson corpora — one file per top-level message under
`notbbg.v1`. Each line is a single record, expected to:

1. Decode via `protojson.Unmarshal` into its proto type without error.
2. Wire-marshal + wire-unmarshal back to a `proto.Equal` value.
3. protojson-round-trip back to a `proto.Equal` value (modulo
   protojson's stable ordering).

Driver: `server/cmd/schemacheck/corpus_test.go` (`go test
./cmd/schemacheck/`). CI runs it as part of `ci.sh`.

## Adding a fixture

1. Pick the correct file (`<message-snake-name>.jsonl`).
2. Append one JSON object per line. Field names follow protojson
   conventions (camelCase by default — protojson accepts both).
3. Re-run `go test ./cmd/schemacheck/`. The corpus test fails noisily
   if the new line doesn't round-trip — usually a typo or a deleted
   proto field.

## Why
- Catch unintended proto field-number renumbering before merge.
- Catch unintended JSON-shape drift when adapters add new fields
  but forget the corresponding string-side change (U6 / U2 etc).
- Keep a human-readable record of the expected shape, indexed by
  topic prefix, for cross-language SDK ports (Phase 10).
