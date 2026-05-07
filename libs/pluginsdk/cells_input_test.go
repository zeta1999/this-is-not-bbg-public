package pluginsdk

import (
	"encoding/json"
	"testing"
)

func TestInputEvent_EditDefault(t *testing.T) {
	// Old wire shape (no kind field) must still decode as an edit.
	raw := []byte(`{"screen_id":"X","address":{"row":1,"col":2},"value":3.14}`)
	var e InputEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e.IsCancel() {
		t.Errorf("empty Kind should not be cancel")
	}
	if e.Kind != "" || e.Address.Row != 1 || e.Address.Col != 2 {
		t.Errorf("unexpected: %+v", e)
	}
}

func TestInputEvent_Cancel(t *testing.T) {
	raw := []byte(`{"kind":"cancel","screen_id":"X","job_id":"job-1"}`)
	var e InputEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if !e.IsCancel() {
		t.Errorf("expected cancel, got %+v", e)
	}
	if e.JobID != "job-1" {
		t.Errorf("JobID=%q", e.JobID)
	}
}

func TestInputEvent_OmitValueWhenCancel(t *testing.T) {
	// Address is a value struct so encoding/json always emits it
	// (omitempty doesn't apply to non-pointer structs). Value DOES
	// omit when nil. Cancel events don't populate Value, so the
	// `"value"` key should not appear on the wire — that's the
	// guarantee downstream subscribers rely on when discriminating
	// edit vs cancel without inspecting Kind.
	e := InputEvent{ScreenID: "X", Kind: "cancel", JobID: "j1"}
	data, _ := json.Marshal(e)
	s := string(data)
	if contains(s, `"value"`) {
		t.Errorf("cancel event should not carry value: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
