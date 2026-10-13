// Package deployment is the shared authorization and durable mock job service.
package deployment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

var ErrDenied = errors.New("permission denied")
var ErrConflict = errors.New("state conflict")
var ErrNotFound = errors.New("not found")
var errNoWork = errors.New("no worker state change")
var Permissions = map[string][]string{
	"Admin":           {"team:run", "team:read", "deployment:read", "deployment:request", "deployment:approve", "agent:manage", "role:manage", "subscriber:manage"},
	"SubscriberAdmin": {"subscriber:manage", "deployment:read", "deployment:request"},
	"TenantAdmin":     {"subscriber:manage:tenant", "tenant:manage:tenant", "deployment:read", "deployment:request"},
	"Diagnostician":   {"team:read", "deployment:read", "deployment:request"},
	"Deployer":        {"deployment:read", "deployment:request", "deployment:start:approved-request"},
}

type Principal struct{ UserID, TenantID, AgentID string }
type Assignment struct {
	UserID   string `json:"userId"`
	TenantID string `json:"tenantId"`
	Role     string `json:"role"`
}
type Agent struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Owner   string    `json:"owner"`
	Tenant  string    `json:"tenant"`
	Host    string    `json:"host"`
	Expires time.Time `json:"expiresAt"`
	Revoked bool      `json:"revoked"`
	Hash    string    `json:"-"`
	Role    string    `json:"role"`
}

// Hash is serialized only in storage, never in HTTP responses.
type StoredAgent struct {
	Agent Agent  `json:"agent"`
	Hash  string `json:"hash"`
}
type Plan struct {
	Interface  string   `json:"interface"`
	Host       string   `json:"host"`
	Commit     string   `json:"commit"`
	ScriptHash string   `json:"scriptHash"`
	Profile    string   `json:"profile"`
	Workdir    string   `json:"workdir"`
	MockOnly   bool     `json:"mockOnly"`
	Changes    []string `json:"changes"`
}
type Grant struct {
	Role       string     `json:"role"`
	RequestID  string     `json:"requestId"`
	JobID      string     `json:"jobId"`
	PlanHash   string     `json:"planHash"`
	Expires    time.Time  `json:"expiresAt"`
	ConsumedAt *time.Time `json:"consumedAt,omitempty"`
}
type Request struct {
	ID         string     `json:"id"`
	Owner      string     `json:"owner"`
	Tenant     string     `json:"tenant"`
	AgentID    string     `json:"agentId"`
	Plan       Plan       `json:"plan"`
	PlanHash   string     `json:"planHash"`
	Created    time.Time  `json:"createdAt"`
	Expires    time.Time  `json:"expiresAt"`
	Status     string     `json:"status"`
	ApprovedBy string     `json:"approvedBy,omitempty"`
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
	JobID      string     `json:"jobId,omitempty"`
	Grant      *Grant     `json:"grant,omitempty"`
}
type Job struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"requestId"`
	Status       string    `json:"status"`
	Stage        string    `json:"stage"`
	Updated      time.Time `json:"updatedAt"`
	Logs         []string  `json:"logs"`
	ExitCode     *int      `json:"exitCode,omitempty"`
	Verification string    `json:"verification,omitempty"`
}
type Audit struct {
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Resource string    `json:"resource"`
}
type State struct {
	Revision    int64                  `json:"revision"`
	Assignments map[string]Assignment  `json:"assignments"`
	Agents      map[string]StoredAgent `json:"agents"`
	Requests    map[string]Request     `json:"requests"`
	Jobs        map[string]Job         `json:"jobs"`
	Audit       []Audit                `json:"audit"`
	Active      string                 `json:"active"`
}

func Empty() State {
	return State{Assignments: map[string]Assignment{}, Agents: map[string]StoredAgent{}, Requests: map[string]Request{}, Jobs: map[string]Job{}, Audit: []Audit{}}
}

type Store interface {
	Load(context.Context) (State, error)
	Save(context.Context, int64, State) error
}
type Service struct {
	Store            Store
	Plan             Plan
	Now              func() time.Time
	StepDelay        time.Duration
	MockFailureStage string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func id(prefix string) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func planHash(p Plan) string { b, _ := json.Marshal(p); return digest(string(b)) }
func (s *Service) update(ctx context.Context, fn func(*State) error) error {
	for i := 0; i < 32; i++ {
		st, err := s.Store.Load(ctx)
		if err != nil {
			return err
		}
		rev := st.Revision
		if err = fn(&st); err != nil {
			return err
		}
		st.Revision = rev + 1
		err = s.Store.Save(ctx, rev, st)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err
	}
	return ErrConflict
}
func event(st *State, now time.Time, p Principal, action, resource string) {
	actor := p.UserID
	if p.AgentID != "" {
		actor += "/" + p.AgentID
	}
	st.Audit = append(st.Audit, Audit{now, actor, action, resource})
}
func validAgent(st State, p Principal, now time.Time) bool {
	a, ok := st.Agents[p.AgentID]
	return ok && !a.Agent.Revoked && now.Before(a.Agent.Expires) && a.Agent.Owner == p.UserID && a.Agent.Tenant == p.TenantID
}
func allowed(st State, p Principal, permission string, now time.Time) bool {
	if p.UserID == "" || p.TenantID == "" {
		return false
	}
	role := "Diagnostician"
	if p.AgentID != "" {
		if !validAgent(st, p, now) {
			return false
		}
	} else if a, ok := st.Assignments[p.UserID]; ok && a.TenantID == p.TenantID {
		role = a.Role
	}
	for _, v := range Permissions[role] {
		if v == permission {
			return true
		}
	}
	return false
}
func visible(st State, p Principal, r Request, now time.Time) bool {
	if !allowed(st, p, "deployment:read", now) {
		return false
	}
	if p.AgentID != "" {
		a := st.Agents[p.AgentID].Agent
		return r.Owner == p.UserID && r.Tenant == p.TenantID && r.AgentID == p.AgentID && r.Plan.Host == a.Host
	}
	return allowed(st, p, "deployment:approve", now) || (r.Owner == p.UserID && r.Tenant == p.TenantID)
}
func (s *Service) Can(ctx context.Context, p Principal, permission string) bool {
	st, e := s.Store.Load(ctx)
	return e == nil && allowed(st, p, permission, s.now())
}
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return Principal{}, e
	}
	h := digest(token)
	for _, a := range st.Agents {
		p := Principal{a.Agent.Owner, a.Agent.Tenant, a.Agent.ID}
		if subtle.ConstantTimeCompare([]byte(a.Hash), []byte(h)) == 1 && validAgent(st, p, s.now()) {
			return p, nil
		}
	}
	return Principal{}, ErrDenied
}
func (s *Service) Bootstrap(ctx context.Context, user, tenant string) error {
	return s.update(ctx, func(st *State) error {
		for _, a := range st.Assignments {
			if a.Role == "Admin" {
				return errors.New("owner already initialized")
			}
		}
		st.Assignments[user] = Assignment{user, tenant, "Admin"}
		event(st, s.now(), Principal{UserID: user}, "owner.bootstrap", user)
		return nil
	})
}
func (s *Service) MigrateLegacyAdmin(ctx context.Context, user, tenant string) error {
	return s.update(ctx, func(st *State) error {
		if _, ok := st.Assignments[user]; !ok {
			st.Assignments[user] = Assignment{user, tenant, "SubscriberAdmin"}
		}
		return nil
	})
}
func (s *Service) Assign(ctx context.Context, p Principal, a Assignment) error {
	return s.update(ctx, func(st *State) error {
		if !allowed(*st, p, "role:manage", s.now()) || p.AgentID != "" {
			return ErrDenied
		}
		if a.Role == "Deployer" || Permissions[a.Role] == nil || a.UserID == "" || a.TenantID == "" {
			return errors.New("invalid role assignment")
		}
		if old, ok := st.Assignments[a.UserID]; ok && old.Role == "Admin" && a.Role != "Admin" {
			n := 0
			for _, x := range st.Assignments {
				if x.Role == "Admin" {
					n++
				}
			}
			if n <= 1 {
				return errors.New("cannot remove last owner")
			}
		}
		st.Assignments[a.UserID] = a
		event(st, s.now(), p, "role.assign", a.UserID)
		return nil
	})
}
func (s *Service) CreateAgent(ctx context.Context, p Principal, name, host string, expires time.Time) (Agent, string, error) {
	a := Agent{ID: id("agent-"), Name: name, Owner: p.UserID, Tenant: p.TenantID, Host: host, Expires: expires, Role: "Diagnostician"}
	token := id("fgc_")
	err := s.update(ctx, func(st *State) error {
		if !allowed(*st, p, "agent:manage", s.now()) || p.AgentID != "" {
			return ErrDenied
		}
		if len(name) < 1 || len(name) > 80 || host != s.Plan.Host || !expires.After(s.now()) || expires.After(s.now().Add(30*24*time.Hour)) {
			return errors.New("invalid agent scope or expiry")
		}
		st.Agents[a.ID] = StoredAgent{a, digest(token)}
		event(st, s.now(), p, "agent.create", a.ID)
		return nil
	})
	if err != nil {
		return Agent{}, "", err
	}
	return a, token, nil
}
func (s *Service) Revoke(ctx context.Context, p Principal, agentID string) error {
	return s.update(ctx, func(st *State) error {
		if !allowed(*st, p, "agent:manage", s.now()) || p.AgentID != "" {
			return ErrDenied
		}
		a, ok := st.Agents[agentID]
		if !ok {
			return ErrNotFound
		}
		a.Agent.Revoked = true
		st.Agents[agentID] = a
		event(st, s.now(), p, "agent.revoke", agentID)
		return nil
	})
}
func (s *Service) Agents(ctx context.Context, p Principal) ([]Agent, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return nil, e
	}
	if !allowed(st, p, "agent:manage", s.now()) || p.AgentID != "" {
		return nil, ErrDenied
	}
	out := []Agent{}
	for _, a := range st.Agents {
		out = append(out, a.Agent)
	}
	return out, nil
}
func (s *Service) Roles(ctx context.Context, p Principal) (map[string]Assignment, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return nil, e
	}
	if !allowed(st, p, "role:manage", s.now()) || p.AgentID != "" {
		return nil, ErrDenied
	}
	return st.Assignments, nil
}
func (s *Service) AuditEvents(ctx context.Context, p Principal) ([]Audit, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return nil, e
	}
	if !allowed(st, p, "role:manage", s.now()) || p.AgentID != "" {
		return nil, ErrDenied
	}
	return st.Audit, nil
}

var ifacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`)

func (s *Service) CreateRequest(ctx context.Context, p Principal, iface string) (Request, error) {
	r := Request{ID: id("req-"), Owner: p.UserID, Tenant: p.TenantID, AgentID: p.AgentID, Plan: s.Plan, Created: s.now(), Expires: s.now().Add(15 * time.Minute), Status: "pending"}
	r.Plan.Interface = iface
	r.PlanHash = planHash(r.Plan)
	err := s.update(ctx, func(st *State) error {
		if !allowed(*st, p, "deployment:request", s.now()) {
			return ErrDenied
		}
		if !ifacePattern.MatchString(iface) || !s.Plan.MockOnly || s.Plan.Commit == "" {
			return errors.New("invalid interface or disabled deployment profile")
		}
		if p.AgentID != "" && st.Agents[p.AgentID].Agent.Host != r.Plan.Host {
			return ErrDenied
		}
		st.Requests[r.ID] = r
		event(st, s.now(), p, "deployment.request", r.ID)
		return nil
	})
	return r, err
}
func (s *Service) Requests(ctx context.Context, p Principal) ([]Request, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return nil, e
	}
	if !allowed(st, p, "deployment:read", s.now()) {
		return nil, ErrDenied
	}
	out := []Request{}
	for _, r := range st.Requests {
		if visible(st, p, r, s.now()) {
			if r.Status == "pending" && !s.now().Before(r.Expires) {
				r.Status = "expired"
			}
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}
func (s *Service) Get(ctx context.Context, p Principal, rid string) (Request, *Job, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return Request{}, nil, e
	}
	r, ok := st.Requests[rid]
	if !ok || !visible(st, p, r, s.now()) {
		return Request{}, nil, ErrNotFound
	}
	if r.Status == "pending" && !s.now().Before(r.Expires) {
		r.Status = "expired"
	}
	var j *Job
	if r.JobID != "" {
		v := st.Jobs[r.JobID]
		j = &v
	}
	return r, j, nil
}
func (s *Service) GetJob(ctx context.Context, p Principal, jid string) (Job, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return Job{}, e
	}
	j, ok := st.Jobs[jid]
	if !ok {
		return Job{}, ErrNotFound
	}
	r := st.Requests[j.RequestID]
	if !visible(st, p, r, s.now()) {
		return Job{}, ErrNotFound
	}
	return j, nil
}
func (s *Service) Approve(ctx context.Context, p Principal, rid, hash string) (Job, error) {
	var out Job
	err := s.update(ctx, func(st *State) error {
		if p.AgentID != "" || !allowed(*st, p, "deployment:approve", s.now()) {
			return ErrDenied
		}
		r, ok := st.Requests[rid]
		if !ok {
			return ErrNotFound
		}
		if hash != r.PlanHash || planHash(r.Plan) != r.PlanHash {
			return errors.New("plan changed; submit a new request")
		}
		if r.JobID != "" {
			out = st.Jobs[r.JobID]
			return nil
		}
		if r.Status != "pending" || !s.now().Before(r.Expires) {
			return errors.New("request not pending or expired")
		}
		if r.AgentID != "" && !validAgent(*st, Principal{r.Owner, r.Tenant, r.AgentID}, s.now()) {
			return ErrDenied
		}
		for _, j := range st.Jobs {
			if j.Status == "queued" || j.Status == "running" {
				return errors.New("host already has an active deployment")
			}
		}
		now := s.now()
		out = Job{ID: id("job-"), RequestID: rid, Status: "queued", Stage: "awaiting worker", Updated: now, Logs: []string{"[MOCK] Approved immutable plan; queued automatically."}}
		r.Status = "queued"
		r.JobID = out.ID
		r.ApprovedBy = p.UserID
		r.ApprovedAt = &now
		r.Grant = &Grant{Role: "Deployer", RequestID: rid, JobID: out.ID, PlanHash: r.PlanHash, Expires: r.Expires}
		st.Requests[rid] = r
		st.Jobs[out.ID] = out
		event(st, now, p, "deployment.approve-and-start", rid)
		return nil
	})
	return out, err
}
func (s *Service) Reject(ctx context.Context, p Principal, rid string) error {
	return s.update(ctx, func(st *State) error {
		if p.AgentID != "" || !allowed(*st, p, "deployment:approve", s.now()) {
			return ErrDenied
		}
		r, ok := st.Requests[rid]
		if !ok {
			return ErrNotFound
		}
		if r.Status != "pending" || !s.now().Before(r.Expires) {
			return ErrConflict
		}
		r.Status = "rejected"
		st.Requests[rid] = r
		event(st, s.now(), p, "deployment.reject", rid)
		return nil
	})
}

// Run uses an atomic claim. A lost running worker is failed, never replayed.
// Revocation stops queued work; running work finishes to avoid unsafe interruption.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}
func (s *Service) tick(ctx context.Context) {
	jid := ""
	err := s.update(ctx, func(st *State) error {
		jid = ""
		changed := false
		for key, r := range st.Requests {
			if r.Status == "pending" && !s.now().Before(r.Expires) {
				r.Status = "expired"
				st.Requests[key] = r
				changed = true
				event(st, s.now(), Principal{UserID: "scheduler"}, "deployment.expired", key)
			}
		}
		if st.Active != "" {
			j := st.Jobs[st.Active]
			if s.now().Sub(j.Updated) > time.Minute {
				s.finish(st, &j, "failed", 1, "Worker heartbeat lost; no automatic retry.")
				return nil
			}
			if changed {
				return nil
			}
			return errNoWork
		}
		for key, j := range st.Jobs {
			if j.Status != "queued" {
				continue
			}
			r := st.Requests[j.RequestID]
			if r.Grant == nil || r.Grant.Role != "Deployer" || r.Grant.RequestID != r.ID || r.Grant.JobID != j.ID || r.Grant.PlanHash != r.PlanHash || r.Grant.ConsumedAt != nil || !s.now().Before(r.Grant.Expires) || r.ApprovedBy == "" {
				s.finish(st, &j, "failed", 1, "Invalid or consumed single-use grant.")
				return nil
			}
			if !s.now().Before(r.Expires) || (r.AgentID != "" && !validAgent(*st, Principal{r.Owner, r.Tenant, r.AgentID}, s.now())) || planHash(r.Plan) != r.PlanHash {
				s.finish(st, &j, "failed", 1, "Approval expired, revoked, or plan changed before execution.")
				return nil
			}
			if !r.Plan.MockOnly {
				s.finish(st, &j, "failed", 1, "Real deployment is disabled.")
				return nil
			}
			jid = key
			st.Active = key
			j.Status = "running"
			j.Stage = "validating"
			j.Updated = s.now()
			st.Jobs[key] = j
			r.Status = "running"
			now := s.now()
			r.Grant.ConsumedAt = &now
			st.Requests[r.ID] = r
			event(st, s.now(), Principal{UserID: "mock-worker"}, "job.claim", key)
			break
		}
		if jid == "" && !changed {
			return errNoWork
		}
		return nil
	})
	if err != nil || jid == "" {
		return
	}
	delay := s.StepDelay
	if delay == 0 {
		delay = time.Second
	}
	for _, stage := range []string{"packages / Go (simulated)", "gtp5g / forwarding / UFW (simulated)", "configuration / compile (simulated)", "mock verification"} {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err = s.update(ctx, func(st *State) error {
			j := st.Jobs[jid]
			if j.Status != "running" || st.Active != jid {
				return ErrConflict
			}
			if s.MockFailureStage == stage {
				s.finish(st, &j, "failed", 42, "Injected mock worker failure at "+stage+"; artifacts retained; no retry.")
				return nil
			}
			j.Stage = stage
			j.Updated = s.now()
			j.Logs = append(j.Logs, "[MOCK] "+stage)
			st.Jobs[jid] = j
			return nil
		}); err != nil {
			return
		}
		if s.MockFailureStage == stage {
			return
		}
	}
	_ = s.update(ctx, func(st *State) error {
		j := st.Jobs[jid]
		if j.Status != "running" || st.Active != jid {
			return ErrConflict
		}
		s.finish(st, &j, "succeeded", 0, "Mock authorization and workflow checks passed. No installation, NF startup or UE test executed.")
		return nil
	})
}
func (s *Service) finish(st *State, j *Job, status string, code int, message string) {
	j.Status = status
	j.Stage = "finished"
	j.Updated = s.now()
	j.ExitCode = &code
	j.Verification = message
	j.Logs = append(j.Logs, "[MOCK] "+message)
	st.Jobs[j.ID] = *j
	r := st.Requests[j.RequestID]
	r.Status = status
	st.Requests[r.ID] = r
	if st.Active == j.ID {
		st.Active = ""
	}
	event(st, s.now(), Principal{UserID: "mock-worker"}, "job."+status, j.ID)
}
func (s *Service) String() string {
	return fmt.Sprintf("deployment profile %s (mock=%v)", s.Plan.Profile, s.Plan.MockOnly)
}

// PermissionsFor returns live, effective permissions, never caller-provided roles.
func (s *Service) PermissionsFor(ctx context.Context, p Principal) ([]string, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return nil, e
	}
	all := map[string]bool{}
	for _, list := range Permissions {
		for _, perm := range list {
			if allowed(st, p, perm, s.now()) {
				all[perm] = true
			}
		}
	}
	out := []string{}
	for perm := range all {
		out = append(out, perm)
	}
	sort.Strings(out)
	return out, nil
}

// HostProtected prevents subscriber/user CRUD from becoming an owner takeover path.
func (s *Service) HostProtected(ctx context.Context, tenant, user string) (bool, error) {
	st, e := s.Store.Load(ctx)
	if e != nil {
		return true, e
	}
	for _, a := range st.Assignments {
		if a.Role == "Admin" && a.TenantID == tenant && (user == "" || a.UserID == user) {
			return true, nil
		}
	}
	return false, nil
}
