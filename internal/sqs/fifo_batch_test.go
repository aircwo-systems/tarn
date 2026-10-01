package sqs

import "testing"

func TestFIFOReceiveBatchesSameGroupAndBlocksFurtherReceives(t *testing.T) {
	store := newTestStore()
	_, err := store.CreateQueue("updates.fifo", map[string]string{"FifoQueue": "true"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"A1", "A2", "A3"} {
		if _, err := store.SendMessage("updates.fifo", body, 0, nil, "A", body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SendMessage("updates.fifo", "B1", 0, nil, "B", "B1"); err != nil {
		t.Fatal(err)
	}

	batch, err := store.ReceiveMessage("updates.fifo", 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(batch); len(got) != 2 || got[0] != "A1" || got[1] != "A2" {
		t.Fatalf("same-group batch=%v, want [A1 A2]", got)
	}
	otherGroup, err := store.ReceiveMessage("updates.fifo", 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(otherGroup); len(got) != 1 || got[0] != "B1" {
		t.Fatalf("while group A is in flight, received %v, want [B1]", got)
	}

	if err := store.DeleteMessage("updates.fifo", batch[1].ReceiptHandle); err != nil {
		t.Fatal(err)
	}
	blocked, err := store.ReceiveMessage("updates.fifo", 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Fatalf("group unblocked while A1 was still in flight: %v", bodies(blocked))
	}
	if err := store.DeleteMessage("updates.fifo", batch[0].ReceiptHandle); err != nil {
		t.Fatal(err)
	}
	remaining, err := store.ReceiveMessage("updates.fifo", 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(remaining); len(got) != 1 || got[0] != "A3" {
		t.Fatalf("after acknowledging the batch, received %v, want [A3]", got)
	}
}
