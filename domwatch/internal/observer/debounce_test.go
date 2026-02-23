package observer

import (
	"testing"

	"github.com/hazyhaar/chrc/domwatch/mutation"
)

func TestCompress_ConsecutiveAttr(t *testing.T) {
	records := []mutation.Record{
		{Op: mutation.OpAttr, XPath: "/div", Name: "class", Value: "a", OldValue: "orig"},
		{Op: mutation.OpAttr, XPath: "/div", Name: "class", Value: "b", OldValue: "a"},
		{Op: mutation.OpAttr, XPath: "/div", Name: "class", Value: "c", OldValue: "b"},
	}

	got := compress(records)
	if len(got) != 1 {
		t.Fatalf("compress: got %d records, want 1", len(got))
	}
	if got[0].Value != "c" {
		t.Errorf("Value: got %q, want %q", got[0].Value, "c")
	}
	if got[0].OldValue != "orig" {
		t.Errorf("OldValue: got %q, want %q", got[0].OldValue, "orig")
	}
}

func TestCompress_ConsecutiveText(t *testing.T) {
	records := []mutation.Record{
		{Op: mutation.OpText, XPath: "/div/text()", Value: "a", OldValue: "orig"},
		{Op: mutation.OpText, XPath: "/div/text()", Value: "b", OldValue: "a"},
		{Op: mutation.OpText, XPath: "/div/text()", Value: "final", OldValue: "b"},
	}

	got := compress(records)
	if len(got) != 1 {
		t.Fatalf("compress: got %d records, want 1", len(got))
	}
	if got[0].Value != "final" {
		t.Errorf("Value: got %q, want %q", got[0].Value, "final")
	}
	if got[0].OldValue != "orig" {
		t.Errorf("OldValue: got %q, want %q", got[0].OldValue, "orig")
	}
}

func TestCompress_InsertNeverCompressed(t *testing.T) {
	records := []mutation.Record{
		{Op: mutation.OpInsert, XPath: "/div/a"},
		{Op: mutation.OpInsert, XPath: "/div/b"},
		{Op: mutation.OpInsert, XPath: "/div/c"},
	}

	got := compress(records)
	if len(got) != 3 {
		t.Fatalf("compress: got %d records, want 3 (inserts never compressed)", len(got))
	}
}

func TestCompress_MixedOps(t *testing.T) {
	records := []mutation.Record{
		{Op: mutation.OpAttr, XPath: "/div", Name: "class", Value: "a", OldValue: "orig"},
		{Op: mutation.OpAttr, XPath: "/div", Name: "class", Value: "b"},
		{Op: mutation.OpInsert, XPath: "/div/span"},
		{Op: mutation.OpText, XPath: "/p/text()", Value: "x", OldValue: "orig2"},
		{Op: mutation.OpText, XPath: "/p/text()", Value: "y"},
		{Op: mutation.OpRemove, XPath: "/div/old"},
	}

	got := compress(records)
	// attr compressed to 1, insert stays, text compressed to 1, remove stays = 4
	if len(got) != 4 {
		t.Fatalf("compress: got %d records, want 4", len(got))
	}
	if got[0].Op != mutation.OpAttr || got[0].Value != "b" {
		t.Errorf("Record[0]: got op=%s value=%s", got[0].Op, got[0].Value)
	}
	if got[1].Op != mutation.OpInsert {
		t.Errorf("Record[1]: got op=%s, want insert", got[1].Op)
	}
	if got[2].Op != mutation.OpText || got[2].Value != "y" {
		t.Errorf("Record[2]: got op=%s value=%s", got[2].Op, got[2].Value)
	}
	if got[3].Op != mutation.OpRemove {
		t.Errorf("Record[3]: got op=%s, want remove", got[3].Op)
	}
}

func TestCompress_Empty(t *testing.T) {
	got := compress(nil)
	if got != nil {
		t.Errorf("compress(nil): got %v, want nil", got)
	}
}

func TestCompress_Single(t *testing.T) {
	records := []mutation.Record{{Op: mutation.OpAttr, XPath: "/div", Name: "x"}}
	got := compress(records)
	if len(got) != 1 {
		t.Fatalf("compress: got %d, want 1", len(got))
	}
}
