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

\* --- Combined next-state relation ---
Next ==
    \/ PublishBulk
    \/ PublishRealtime
    \/ SendRealtime
    \/ SendBulk
    \/ WaitForCredits
    \/ ClientAck

\* --- Fairness: eventually the client will ack ---
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

=========================================================================
\* Config for TLC model checker:
\* CONSTANTS MaxCredits = 4, CreditRefill = 2, BulkBufSize = 6, RealtimeBufSize = 4
\* INVARIANT Safety
\* PROPERTY NoDeadlock
