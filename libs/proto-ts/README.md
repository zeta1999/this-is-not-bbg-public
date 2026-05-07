# proto-ts

Typed surfaces for the JS side, generated from the protos under
`proto/notbbg/v1/`. Two trees here:

- `gen/` — `protoc-gen-es` output (TypeScript types matching every
  message in `types.proto`, `feeds.proto`, `server.proto`,
  `plugin.proto`, `auth.proto`). U4 (2026-04-25). Regenerated via
  `make proto-gen` / `buf generate`. Don't hand-edit; the wire
  shape is the source of truth.
- `schema/` — JSON Schemas for the Go config structs (server config
  + adapter configs). Reflected by `cmd/schema-gen`. Used by
  desktop / phone config-editing UIs to validate before sending
  the config back to the server.

## Consuming from desktop / phone

```ts
import type { OHLC } from "../../libs/proto-ts/gen/notbbg/v1/types_pb.js";
```

Note the `.js` suffix — `protoc-gen-es` emits ESM where TS files
import each other with explicit `.js` extensions, which Vite +
Expo both accept.

## Migration plan

The desktop/phone codebases still hand-roll many of these types
(`desktop/src/types.ts`, `phone/app/(tabs)/plugins.tsx`). Migrating
them to the codegenned types is mechanical but cosmetic — track as
a follow-up; U4 ships with the codegen wired so further migrations
land in small PRs.
