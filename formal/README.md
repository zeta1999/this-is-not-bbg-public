# Formal Verification

## TLA+ Spec: Credit-Based Backpressure Protocol

`backpressure.tla` models the server→client data flow with:
- **Splitter**: routes bus messages to realtime (LOB, trades) or bulk (OHLC) queues
- **Sender**: priority select — realtime always first, bulk gated by credits
- **Client**: sends credit acks to replenish bulk credits

### Properties Verified

| Property | Description |
|----------|-------------|
| Safety (TypeOK) | All variables stay in their type domains |
| BulkBounded | Bulk queue never exceeds buffer size |
| RealtimeBounded | Realtime queue never exceeds buffer size |
| CreditsNonNeg | Credits never go negative |
| NoDeadlock | System can always make progress |
| RealtimeProgress | Realtime messages are eventually sent |

### Run Model Checker

```bash
# Install TLA+ toolbox or use CLI:
# https://github.com/tlaplus/tlaplus/releases

# Check with small constants (fast):
tlc backpressure.tla -config backpressure.cfg

# Or use the TLA+ Toolbox GUI:
# File → Open Spec → backpressure.tla
# Model → New Model → Set constants from .cfg → Run
```

### Constants

Small values for model checking (state space explosion with large values):
- `MaxCredits = 4` (production: 512)
- `CreditRefill = 2` (production: 256)
- `BulkBufSize = 6` (production: 8192)
- `RealtimeBufSize = 4` (production: 1024)

The protocol properties hold for any positive constant values.
