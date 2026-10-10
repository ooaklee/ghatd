package teleprovider

import (
	"context"
	"errors"
	"testing"
)

// exampleSink is an illustrative consumer ledger, deliberately test-only. Production adoption must
// implement accept and checkpoint behind its owning repository ports. In-memory
// maps and cursors in this example do not provide crash/restart guarantees.
type exampleSink struct {
	// accepted deduplicates rows by session and storage identity within this process only.
	accepted map[string]bool
	// cursor retains the older backlog position independently of newest-page rescans.
	cursor string
	// fail simulates rejected page acceptance without advancing the checkpoint.
	fail bool
	// events records acceptance/checkpoint order for the consumer-boundary assertions.
	events []string
}

// accept records session-scoped row acceptance, or fails before changing the illustrative ledger.
func (s *exampleSink) accept(messages []Message) error {
	if s.fail {
		return errors.New("sink unavailable")
	}
	for _, m := range messages {
		s.accepted[m.SessionID+":"+m.RowID] = true
		s.events = append(s.events, "accept:"+m.RowID)
	}
	return nil
}

// checkpoint records the older-page cursor after acceptance so tests can assert operation order.
func (s *exampleSink) checkpoint(cursor string) {
	s.cursor = cursor
	s.events = append(s.events, "checkpoint:"+cursor)
}

// examplePoll rescans the newest page before continuing an older backlog.
// A bounded pass is not a complete history assertion. The caller schedules the
// next pass, preserves its dedupe ledger, and separately reports retention gaps.
func examplePoll(ctx context.Context, list func(context.Context, MessageQuery) (MessagePage, error), sink *exampleSink, maxOlderPages int) error {
	first, err := list(ctx, MessageQuery{ChatID: group, Limit: 2})
	if err != nil {
		return err
	}
	if err = sink.accept(first.Messages); err != nil {
		return err
	}
	cursor := sink.cursor
	if cursor == "" {
		cursor = first.NextCursor
		sink.checkpoint(cursor)
	}
	for n := 0; cursor != "" && n < maxOlderPages; n++ {
		page, err := list(ctx, MessageQuery{ChatID: group, Limit: 2, After: cursor})
		if err != nil {
			return err
		} // Invalid/expired cursor is a visible gap, never success.
		if err = sink.accept(page.Messages); err != nil {
			return err
		}
		cursor = page.NextCursor
		sink.checkpoint(cursor)
	}
	return nil
}

// This ordered consumer lifecycle is intentionally standalone: later passes
// depend on retained receipts/cursors from earlier arrivals and a failed sink
// must preserve that same checkpoint. Independent fixtures would lose the contract.
func TestConsumerNewestRescanBacklogAndAcceptanceBeforeCheckpoint(t *testing.T) {
	makeMessage := func(id string) Message {
		return Message{RowID: id, MessageID: "wa_" + id, SessionID: session, ChatID: group}
	}
	calls := []string{}
	arrival := false
	list := func(ctx context.Context, q MessageQuery) (MessagePage, error) {
		calls = append(calls, q.After)
		switch q.After {
		case "":
			if arrival {
				return MessagePage{Messages: []Message{makeMessage("new")}, NextCursor: "top", HasMore: true}, nil
			}
			return MessagePage{Messages: []Message{makeMessage("top")}, NextCursor: "top", HasMore: true}, nil
		case "top":
			arrival = true
			return MessagePage{Messages: []Message{makeMessage("middle")}, NextCursor: "middle", HasMore: true}, nil
		case "middle":
			return MessagePage{Messages: []Message{makeMessage("late-backfill")}}, nil
		default:
			return MessagePage{}, errors.New("retention gap")
		}
	}
	sink := &exampleSink{accepted: map[string]bool{}}
	if err := examplePoll(context.Background(), list, sink, 1); err != nil {
		t.Fatal(err)
	}
	if sink.cursor != "middle" || sink.accepted[session+":new"] {
		t.Fatal(sink)
	}
	if err := examplePoll(context.Background(), list, sink, 1); err != nil {
		t.Fatal(err)
	}
	if calls[2] != "" || calls[3] != "middle" || !sink.accepted[session+":new"] || !sink.accepted[session+":late-backfill"] || sink.cursor != "" {
		t.Fatal(calls, sink)
	}
	// Repeated reads are accepted by stable session/row identity, not timestamps.
	before := len(sink.accepted)
	if err := examplePoll(context.Background(), list, sink, 2); err != nil {
		t.Fatal(err)
	}
	if len(sink.accepted) != before {
		t.Fatal("dedupe failed")
	}
	sink.cursor = "expired"
	if err := examplePoll(context.Background(), list, sink, 1); err == nil {
		t.Fatal("silently swallowed retention gap")
	}
	if sink.cursor != "expired" {
		t.Fatal("advanced across gap")
	}
	sink.cursor = "middle"
	sink.fail = true
	beforeEvents := len(sink.events)
	if err := examplePoll(context.Background(), list, sink, 1); err == nil {
		t.Fatal("sink failure ignored")
	}
	if sink.cursor != "middle" || len(sink.events) != beforeEvents {
		t.Fatal("checkpoint advanced before acceptance")
	}
}
