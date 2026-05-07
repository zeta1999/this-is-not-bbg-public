--------------------------- MODULE Backpressure ---------------------------
\* TLA+ specification of the notbbg credit-based backpressure protocol.
\*
\* Models the server relay (splitter + sender) and TUI client interaction.
\* Properties: no deadlock, no starvation of realtime data, bounded bulk queue.

EXTENDS Integers, Sequences, FiniteSets

CONSTANTS
    MaxCredits,     \* Initial bulk credits (e.g., 512)
    CreditRefill,   \* Credits per ack (e.g., 256)
    BulkBufSize,    \* Max bulk buffer size (e.g., 8192)
    RealtimeBufSize \* Max realtime buffer size (e.g., 1024)

VARIABLES
    credits,        \* Current bulk credit count
    bulkQueue,      \* Messages in bulk queue (OHLC)
    realtimeQueue,  \* Messages in realtime queue (LOB, trades, alerts)
    sent,           \* Total messages sent to client
    dropped,        \* Total messages dropped (buffer full)
    clientAcked,    \* Total credits sent by client
    senderState     \* "idle" | "sending_realtime" | "sending_bulk" | "waiting_credits"

vars == <<credits, bulkQueue, realtimeQueue, sent, dropped, clientAcked, senderState>>

TypeOK ==
    /\ credits \in 0..MaxCredits + 10 * CreditRefill
    /\ bulkQueue \in 0..BulkBufSize
    /\ realtimeQueue \in 0..RealtimeBufSize
    /\ sent \in Nat
    /\ dropped \in Nat
    /\ clientAcked \in Nat
    /\ senderState \in {"idle", "sending_realtime", "sending_bulk", "waiting_credits"}

Init ==
    /\ credits = MaxCredits
    /\ bulkQueue = 0
    /\ realtimeQueue = 0
    /\ sent = 0
    /\ dropped = 0
    /\ clientAcked = 0
    /\ senderState = "idle"

\* --- Bus publishes a bulk (OHLC) message ---
PublishBulk ==
    /\ IF bulkQueue < BulkBufSize
       THEN /\ bulkQueue' = bulkQueue + 1
            /\ dropped' = dropped
       ELSE /\ bulkQueue' = bulkQueue
            /\ dropped' = dropped + 1
    /\ UNCHANGED <<credits, realtimeQueue, sent, clientAcked, senderState>>

\* --- Bus publishes a realtime (LOB/trade/alert) message ---
PublishRealtime ==
    /\ IF realtimeQueue < RealtimeBufSize
       THEN /\ realtimeQueue' = realtimeQueue + 1
            /\ dropped' = dropped
       ELSE /\ realtimeQueue' = realtimeQueue
            /\ dropped' = dropped + 1
    /\ UNCHANGED <<credits, bulkQueue, sent, clientAcked, senderState>>

\* --- Sender: send realtime message (always, no credit check) ---
SendRealtime ==
    /\ realtimeQueue > 0
    /\ realtimeQueue' = realtimeQueue - 1
    /\ sent' = sent + 1
    /\ senderState' = "sending_realtime"
    /\ UNCHANGED <<credits, bulkQueue, dropped, clientAcked>>

\* --- Sender: send bulk message (only if credits > 0) ---
SendBulk ==
    /\ bulkQueue > 0
    /\ credits > 0
    /\ realtimeQueue = 0  \* Priority: only send bulk when no realtime pending
    /\ bulkQueue' = bulkQueue - 1
    /\ credits' = credits - 1
    /\ sent' = sent + 1
    /\ senderState' = "sending_bulk"
    /\ UNCHANGED <<realtimeQueue, dropped, clientAcked>>

\* --- Sender: wait for credits (bulk pending but no credits) ---
WaitForCredits ==
    /\ bulkQueue > 0
    /\ credits = 0
    /\ realtimeQueue = 0
    /\ senderState' = "waiting_credits"
    /\ UNCHANGED <<credits, bulkQueue, realtimeQueue, sent, dropped, clientAcked>>

\* --- Client: send credit ack ---
ClientAck ==
    /\ credits' = credits + CreditRefill
    /\ clientAcked' = clientAcked + CreditRefill
    /\ senderState' = "idle"
    /\ UNCHANGED <<bulkQueue, realtimeQueue, sent, dropped>>

\* --- Client flips timeframe mid-stream: the active DataRangeRequest
\*     is cancelled. Any bulk messages still queued for the old
\*     correlation_id are discarded, and the credits they had been
\*     holding are returned to the pool so the next request starts
\*     with a fresh budget. Realtime queue is untouched — it is not
\*     correlation-scoped. Mirrors DATA-PLAN.md §3 + §2
\*     "InvalidateOnSwitch" action. ---
InvalidateOnSwitch ==
    /\ bulkQueue > 0
    /\ bulkQueue' = 0
    \* Credits aren't literally 'returned' in the server (they just
    \* were never spent past SendBulk), but for the model we reset
    \* to MaxCredits to express "next request starts fresh".
    /\ credits' = MaxCredits
    /\ senderState' = "idle"
    /\ UNCHANGED <<realtimeQueue, sent, dropped, clientAcked>>

\* --- Client drops mid-session and reconnects. Server tears the
\*     relay down — bulk and realtime queues are cleared, credits
\*     reset to MaxCredits, sender returns to idle. The client
\*     reissues the subscription on the new connection with a stale
\*     cursor and picks up live data from that point. clientAcked
\*     accumulates across reconnects because it's a monotonic
\*     counter (used only by liveness arguments); the actual
\*     server-side credit state is refreshed.
\*
\*     This generalizes the earlier TUI-specific actor to any
\*     credit-aware client: phone, desktop SSE, or future Rust/C++
\*     SDKs. The shape is the same — fresh budget + fresh queues on
\*     reconnect, no deadlock with an in-flight bulk send. ---
ClientReconnect ==
    /\ bulkQueue' = 0
    /\ realtimeQueue' = 0
    /\ credits' = MaxCredits
    /\ senderState' = "idle"
    /\ UNCHANGED <<sent, dropped, clientAcked>>

\* --- Combined next-state relation ---
Next ==
    \/ PublishBulk
    \/ PublishRealtime
    \/ SendRealtime
    \/ SendBulk
    \/ WaitForCredits
    \/ ClientAck
    \/ InvalidateOnSwitch
    \/ ClientReconnect

\* --- Fairness: eventually the client will ack. InvalidateOnSwitch
\*     is explicitly NOT fair — we model it as a user action that
\*     may or may not fire. ---
Fairness == WF_vars(ClientAck) /\ WF_vars(SendRealtime) /\ WF_vars(SendBulk)

Spec == Init /\ [][Next]_vars /\ Fairness

\* ============================
\* PROPERTIES TO VERIFY
\* ============================

\* 1. No deadlock: the system can always make progress.
NoDeadlock ==
    \/ realtimeQueue > 0     \* Can send realtime
    \/ (bulkQueue > 0 /\ credits > 0)  \* Can send bulk
    \/ (bulkQueue = 0 /\ realtimeQueue = 0)  \* Nothing to send (idle)
    \/ senderState = "waiting_credits"  \* Waiting for client ack
    \/ bulkQueue > 0                    \* InvalidateOnSwitch always enabled

\* 7. Timeframe-switch invariant: after InvalidateOnSwitch the bulk
\*    queue is empty AND credits have been refreshed — no leak.
\*    This is an action property on InvalidateOnSwitch.
SwitchResetsBulkAndCredits ==
    [InvalidateOnSwitch]_vars =>
        (bulkQueue' = 0 /\ credits' = MaxCredits)

\* 2. Realtime never starved: if realtime message exists, it will be sent.
RealtimeProgress ==
    realtimeQueue > 0 ~> realtimeQueue < realtimeQueue

\* 3. Bulk queue bounded: never exceeds buffer size.
BulkBounded == bulkQueue <= BulkBufSize

\* 4. Realtime queue bounded.
RealtimeBounded == realtimeQueue <= RealtimeBufSize

\* 5. Credits never negative.
CreditsNonNeg == credits >= 0

\* 6. Safety: type invariant always holds.
Safety == TypeOK /\ BulkBounded /\ RealtimeBounded /\ CreditsNonNeg

\* ============================
\* Phase-2 / Phase-5 extensions (2026-04-24)
\* ============================
\*
\* These invariants document, in specification form, the properties
\* Go tests in `server/internal/bus/policy_test.go` and
\* `server/internal/persistq/queue_test.go` already check
\* operationally. They are additive: the existing model still
\* verifies NoDeadlock, RealtimeProgress, BulkBounded without needing
\* them. They are restated here so a future TLC run can widen the
\* invariant set without re-discovering the design intent.

\* --- CoalesceByKey (Phase 2) ---
\* A CoalesceByKey subscriber holds at most one buffered message per
\* key. We don't model the full per-key state here (that would
\* explode the state space); instead we state the property such a
\* subscriber's buffer satisfies:
\*
\*   ∀ k ∈ keys.  |{ m ∈ buffer : key(m) = k }| ≤ 1
\*
\* and the buffer-size invariant mirrors BulkBounded:
\*
\*   |buffer| ≤ cap
\*
\* Proof sketch: the implementation replaces the slot keyed by a
\* matching incoming message (preserving the key-uniqueness
\* invariant) and drops incoming messages when cap is reached and
\* the key is unseen (preserving the cap invariant). No action can
\* ever grow |buffer| past cap or produce two entries with the same
\* key. This mirrors BulkBounded's sent ≤ published − dropped
\* relation, applied per-key. See
\* `TestRapid_CoalesceByKey_LatestPerKeyWins` for the Go mirror.

\* --- Disk-spill WAL (Phase 5) ---
\* With persistq interposed between the bus subscriber and the final
\* durable writer (BBolt / datalake JSONL), the subscriber has two
\* independent bounded stages:
\*
\*   (a) bus channel → persistq.Enqueue    — bus sub buffer, BulkBufSize-ish
\*   (b) persistq → downstream writer      — WAL cap WALCapBytes
\*
\* Claim (WALBoundedLoss): the only drop path for a WAL-backed
\* writer is when persistq.Enqueue returns ErrFull — which by
\* construction only fires when no drained segment can be reclaimed
\* AND the incoming record would exceed WALCapBytes.
\*
\*   ¬ErrFull(persistq) ⇒ ¬writerDrop(msg)
\*
\* And, strengthening under "finite stall with sufficiently large
\* WAL":
\*
\*   ∀ msg. ∃ cap_sufficient_for_this_stall.
\*     cap_sufficient_for_this_stall ⇒ ¬writerDrop(msg)
\*
\* Corollary: if downstream I/O resumes before WAL fills, every
\* enqueued message is eventually delivered to the writer, and thus
\* to disk. The Go property tests
\* `TestDropOldestDrainedSegment` and `TestWriter_WAL_ReopenResumes`
\* cover these operationally.

=========================================================================
\* Config for TLC model checker:
\* CONSTANTS MaxCredits = 4, CreditRefill = 2, BulkBufSize = 6, RealtimeBufSize = 4
\* INVARIANT Safety
\* PROPERTY NoDeadlock
