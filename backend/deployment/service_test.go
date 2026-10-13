package deployment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, Principal, Principal, string) {
	t.Helper()
	s := &Service{Store: NewMemoryStore(), Plan: Plan{Host: "test-host", Commit: strings.Repeat("a", 40), ScriptHash: strings.Repeat("b", 64), MockOnly: true, Profile: "mock-v1"}, StepDelay: time.Millisecond}
	admin := Principal{UserID: "owner", TenantID: "tenant-a"}
	if e := s.Bootstrap(context.Background(), admin.UserID, admin.TenantID); e != nil {
		t.Fatal(e)
	}
	a, token, e := s.CreateAgent(context.Background(), admin, "test-agent", "test-host", time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	return s, admin, Principal{admin.UserID, admin.TenantID, a.ID}, token
}
func TestAuthorizationBoundaries(t *testing.T) {
	ctx := context.Background()
	s, admin, agent, token := fixture(t)
	p, e := s.Authenticate(ctx, token)
	if e != nil || p != agent {
		t.Fatal("credential identity")
	}
	if _, e = s.Authenticate(ctx, "invalid"); !errors.Is(e, ErrDenied) {
		t.Fatal("invalid credential accepted")
	}
	r, e := s.CreateRequest(ctx, agent, "enp0s3")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []Principal{agent, {UserID: "other", TenantID: "tenant-a"}, {UserID: admin.UserID, TenantID: "other"}} {
		if _, e = s.Approve(ctx, p, r.ID, r.PlanHash); !errors.Is(e, ErrDenied) {
			t.Fatal("unauthorized approval", p, e)
		}
		if e = s.Assign(ctx, p, Assignment{"other", "tenant-a", "Admin"}); !errors.Is(e, ErrDenied) {
			t.Fatal("self elevation", e)
		}
		if _, _, e = s.CreateAgent(ctx, p, "evil", "test-host", time.Now().Add(time.Hour)); !errors.Is(e, ErrDenied) {
			t.Fatal("unauthorized agent management")
		}
	}
	for _, p := range []Principal{{UserID: "other", TenantID: "tenant-a"}, {UserID: admin.UserID, TenantID: "other"}} {
		if _, _, e = s.Get(ctx, p, r.ID); e == nil {
			t.Fatal("cross-scope read")
		}
	}
	second, _, e := s.CreateAgent(ctx, admin, "other session", "test-host", time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Get(ctx, Principal{admin.UserID, admin.TenantID, second.ID}, r.ID); e == nil {
		t.Fatal("Agent sessions not isolated")
	}
	if _, _, e = s.CreateAgent(ctx, admin, "scope", "another-host", time.Now().Add(time.Hour)); e == nil {
		t.Fatal("scope accepted")
	}
	if e = s.MigrateLegacyAdmin(ctx, "legacy", "legacy-tenant"); e != nil {
		t.Fatal(e)
	}
	legacy := Principal{UserID: "legacy", TenantID: "legacy-tenant"}
	if s.Can(ctx, legacy, "deployment:approve") || !s.Can(ctx, legacy, "subscriber:manage") {
		t.Fatal("legacy host elevation")
	}
}
func TestApproveAutoRunsAndReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, admin, agent, _ := fixture(t)
	r, _ := s.CreateRequest(ctx, agent, "eth0")
	if _, e := s.Approve(ctx, admin, r.ID, "changed"); e == nil {
		t.Fatal("plan mismatch accepted")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); j, e := s.Approve(ctx, admin, r.ID, r.PlanHash); ids <- j.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatal("duplicate jobs", unique)
	}
	next, _ := s.CreateRequest(ctx, agent, "eth1")
	if _, e := s.Approve(ctx, admin, next.ID, next.PlanHash); e == nil {
		t.Fatal("concurrent host jobs")
	}
	second := &Service{Store: s.Store, Plan: s.Plan, StepDelay: time.Millisecond}
	wg.Add(2)
	go func() { defer wg.Done(); s.tick(ctx) }()
	go func() { defer wg.Done(); second.tick(ctx) }()
	wg.Wait()
	got, j, e := s.Get(ctx, agent, r.ID)
	if e != nil || got.Status != "succeeded" || j.ExitCode == nil || *j.ExitCode != 0 {
		t.Fatal("automatic execution failed", got, j, e)
	}
	replay, e := s.Approve(ctx, admin, r.ID, r.PlanHash)
	if e != nil || replay.ID != j.ID {
		t.Fatal("replayed grant")
	}
	st, _ := s.Store.Load(ctx)
	claims := 0
	for _, event := range st.Audit {
		if event.Action == "job.claim" {
			claims++
		}
	}
	if claims != 1 {
		t.Fatal("execution count", claims)
	}
	if _, e := s.GetJob(ctx, Principal{UserID: "outsider", TenantID: "tenant-a"}, j.ID); e == nil {
		t.Fatal("job/log scope bypass")
	}
	svcRestart := Service{Store: s.Store}
	if !svcRestart.Can(ctx, admin, "deployment:approve") {
		t.Fatal("role lost on restart")
	}
	if e := svcRestart.Bootstrap(ctx, "someone", "tenant"); e == nil {
		t.Fatal("owner reset allowed")
	}
}
func TestExpiredRevokedTamperedAndCrash(t *testing.T) {
	ctx := context.Background()
	t.Run("rejected", func(t *testing.T) {
		s, a, p, _ := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		if e := s.Reject(ctx, a, r.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Approve(ctx, a, r.ID, r.PlanHash); e == nil {
			t.Fatal("rejected replay")
		}
	})
	t.Run("expired", func(t *testing.T) {
		s, a, p, _ := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		s.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
		if _, e := s.Approve(ctx, a, r.ID, r.PlanHash); e == nil {
			t.Fatal("expired approval")
		}
	})
	t.Run("revoked queued", func(t *testing.T) {
		s, a, p, token := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		_, _ = s.Approve(ctx, a, r.ID, r.PlanHash)
		if e := s.Revoke(ctx, a, p.AgentID); e != nil {
			t.Fatal(e)
		}
		s.tick(ctx)
		_, j, e := s.Get(ctx, a, r.ID)
		if e != nil || j.Status != "failed" {
			t.Fatal("revoked job ran", e)
		}
		if _, e = s.Authenticate(ctx, token); e == nil {
			t.Fatal("revoked token")
		}
	})
	t.Run("expired queued", func(t *testing.T) {
		s, a, p, _ := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		_, _ = s.Approve(ctx, a, r.ID, r.PlanHash)
		s.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
		s.tick(ctx)
		_, j, _ := s.Get(ctx, a, r.ID)
		if j.Status != "failed" {
			t.Fatal("expired queued ran")
		}
	})
	t.Run("tampered storage", func(t *testing.T) {
		s, a, p, _ := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		_, _ = s.Approve(ctx, a, r.ID, r.PlanHash)
		_ = s.update(ctx, func(st *State) error {
			v := st.Requests[r.ID]
			v.Plan.Interface = "evil"
			st.Requests[r.ID] = v
			return nil
		})
		s.tick(ctx)
		_, j, _ := s.Get(ctx, a, r.ID)
		if j.Status != "failed" {
			t.Fatal("changed plan executed")
		}
	})
	t.Run("lost worker no replay", func(t *testing.T) {
		s, a, p, _ := fixture(t)
		r, _ := s.CreateRequest(ctx, p, "eth0")
		j, _ := s.Approve(ctx, a, r.ID, r.PlanHash)
		_ = s.update(ctx, func(st *State) error {
			v := st.Jobs[j.ID]
			v.Status = "running"
			v.Updated = time.Now().Add(-2 * time.Minute)
			st.Active = j.ID
			st.Jobs[j.ID] = v
			return nil
		})
		s.tick(ctx)
		_, got, _ := s.Get(ctx, a, r.ID)
		if got.Status != "failed" {
			t.Fatal("lost worker replay")
		}
	})
	t.Run("last owner", func(t *testing.T) {
		s, a, _, _ := fixture(t)
		if e := s.Assign(ctx, a, Assignment{a.UserID, a.TenantID, "Diagnostician"}); e == nil {
			t.Fatal("last owner removed")
		}
	})
}

func TestMockFailurePreservesResultAndCannotReplay(t *testing.T) {
	ctx := context.Background()
	s, a, p, _ := fixture(t)
	s.MockFailureStage = "configuration / compile (simulated)"
	r, _ := s.CreateRequest(ctx, p, "eth0")
	_, _ = s.Approve(ctx, a, r.ID, r.PlanHash)
	s.tick(ctx)
	_, job, e := s.Get(ctx, p, r.ID)
	if e != nil || job.Status != "failed" || job.ExitCode == nil || *job.ExitCode != 42 || !strings.Contains(job.Verification, "artifacts retained") {
		t.Fatal("mock failure evidence", job, e)
	}
	replay, e := s.Approve(ctx, a, r.ID, r.PlanHash)
	if e != nil || replay.ID != job.ID {
		t.Fatal("failure replay", e)
	}
	s.tick(ctx)
	_, job, _ = s.Get(ctx, p, r.ID)
	if job.Status != "failed" {
		t.Fatal("failed job retried")
	}
}

func TestRunningRevocationCompletesAndGrantIsConsumed(t *testing.T) {
	ctx := context.Background()
	s, a, p, _ := fixture(t)
	s.StepDelay = 10 * time.Millisecond
	r, _ := s.CreateRequest(ctx, p, "eth0")
	_, _ = s.Approve(ctx, a, r.ID, r.PlanHash)
	done := make(chan struct{})
	go func() { s.tick(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		st, _ := s.Store.Load(ctx)
		if st.Requests[r.ID].Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not claim")
		}
		time.Sleep(time.Millisecond)
	}
	if e := s.Revoke(ctx, a, p.AgentID); e != nil {
		t.Fatal(e)
	}
	<-done
	got, job, e := s.Get(ctx, a, r.ID)
	if e != nil || job.Status != "succeeded" || got.Grant == nil || got.Grant.ConsumedAt == nil {
		t.Fatal("running revocation or grant consumption", e)
	}
	if _, _, e = s.Get(ctx, p, r.ID); e == nil {
		t.Fatal("revoked Agent still reads")
	}
}
func TestSchedulerPersistsExpiryAndIdleDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	s, _, p, _ := fixture(t)
	r, _ := s.CreateRequest(ctx, p, "eth0")
	before, _ := s.Store.Load(ctx)
	s.tick(ctx)
	after, _ := s.Store.Load(ctx)
	if before.Revision != after.Revision {
		t.Fatal("idle writes")
	}
	s.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	s.tick(ctx)
	after, _ = s.Store.Load(ctx)
	if after.Requests[r.ID].Status != "expired" {
		t.Fatal("expiry not persisted")
	}
}

func TestTenantRolesAndHostCredentialProtection(t *testing.T) {
	ctx := context.Background()
	s, a, _, _ := fixture(t)
	p := Principal{UserID: "tenant-manager", TenantID: "tenant-b"}
	if e := s.Assign(ctx, a, Assignment{p.UserID, p.TenantID, "TenantAdmin"}); e != nil {
		t.Fatal(e)
	}
	if s.Can(ctx, p, "subscriber:manage") || s.Can(ctx, p, "deployment:approve") || !s.Can(ctx, p, "tenant:manage:tenant") {
		t.Fatal("tenant role became global")
	}
	protected, e := s.HostProtected(ctx, a.TenantID, a.UserID)
	if e != nil || !protected {
		t.Fatal("owner credentials unprotected")
	}
	if protected, _ = s.HostProtected(ctx, a.TenantID, "ordinary-user"); protected {
		t.Fatal("ordinary credentials mistaken for host owner")
	}
}
