package team

import (
	"context"
	"testing"
)

func TestInvalidPromptDoesNotStartModel(t *testing.T) {
	s := Service{}
	for _, prompt := range []string{"", "   "} {
		if _, e := s.Start(context.Background(), "owner", "tenant", prompt); e == nil {
			t.Fatal("invalid prompt accepted")
		}
	}
}
func TestCancellationOnlyTargetsOwnedRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := Service{active: "owned", cancel: cancel}
	if e := s.Cancel("another"); e == nil || ctx.Err() != nil {
		t.Fatal("wrong run was cancelled")
	}
	if e := s.Cancel("owned"); e != nil || ctx.Err() == nil {
		t.Fatal("active run was not cancelled", e)
	}
}
