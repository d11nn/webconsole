// Package team persists real Codex team decisions and observed MCP calls.
package team

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Event struct {
	Seq     int             `json:"seq" bson:"seq"`
	Type    string          `json:"type" bson:"type"`
	AgentID string          `json:"agentId" bson:"agentId"`
	At      string          `json:"at" bson:"at"`
	Data    json.RawMessage `json:"data" bson:"data"`
}
type Run struct {
	ID      string    `json:"id" bson:"_id"`
	Owner   string    `json:"owner" bson:"owner"`
	Tenant  string    `json:"tenant" bson:"tenant"`
	Prompt  string    `json:"prompt" bson:"prompt"`
	Status  string    `json:"status" bson:"status"`
	Created time.Time `json:"createdAt" bson:"createdAt"`
	Updated time.Time `json:"updatedAt" bson:"updatedAt"`
	Mode    string    `json:"mode" bson:"mode"`
	Events  []Event   `json:"events" bson:"events"`
	Summary string    `json:"summary,omitempty" bson:"summary"`
	Error   string    `json:"error,omitempty" bson:"error"`
}
type Service struct {
	Collection *mongo.Collection
	Runtime    string
	Catalog    json.RawMessage
	mu         sync.Mutex
	wg         sync.WaitGroup
	active     string
	closed     bool
	cancel     context.CancelFunc
}

var ErrBusy = errors.New("another team run is active")

func New(ctx context.Context, db *mongo.Database, runtime string, catalog json.RawMessage) (*Service, error) {
	s := &Service{Collection: db.Collection("agentTeamRuns"), Runtime: runtime, Catalog: catalog}
	// Each configured Webconsole team runtime is a single owner process.
	// Interrupted runs retain their evidence and are never automatically replayed.
	_, e := s.Collection.UpdateMany(ctx, bson.M{"status": bson.M{"$in": []string{"queued", "running"}}}, bson.M{"$set": bson.M{"status": "interrupted", "error": "Webconsole restarted; no automatic replay", "updatedAt": time.Now().UTC()}})
	return s, e
}
func (s *Service) Start(ctx context.Context, owner, tenant, prompt string) (Run, error) {
	if len(strings.TrimSpace(prompt)) == 0 || len(prompt) > 12000 {
		return Run{}, errors.New("invalid prompt length")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Run{}, errors.New("team runtime is shutting down")
	}
	if s.active != "" {
		return Run{}, ErrBusy
	}
	now := time.Now().UTC()
	r := Run{ID: uuid.NewString(), Owner: owner, Tenant: tenant, Prompt: prompt, Status: "queued", Created: now, Updated: now, Mode: "real-model-plan-only", Events: []Event{}}
	if _, e := s.Collection.InsertOne(ctx, r); e != nil {
		return Run{}, e
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	s.active = r.ID
	s.cancel = cancel
	s.wg.Add(1)
	go s.execute(runCtx, r.ID, prompt)
	return r, nil
}
func (s *Service) execute(ctx context.Context, id, prompt string) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if s.active == id {
			if s.cancel != nil {
				s.cancel()
			}
			s.active = ""
			s.cancel = nil
		}
		s.mu.Unlock()
	}()
	state := "failed"
	message := "Runtime failed; no installation was executed"
	summary := ""
	if ctx.Err() != nil {
		_ = s.set(id, bson.M{"status": "cancelled", "error": "Run cancelled before startup"})
		return
	}
	if e := s.set(id, bson.M{"status": "running", "updatedAt": time.Now().UTC()}); e != nil {
		return
	}
	input, _ := json.Marshal(map[string]string{"prompt": prompt})
	command := exec.CommandContext(ctx, "python3", s.Runtime)
	command.Stdin = strings.NewReader(string(input))
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	command.WaitDelay = 10 * time.Second
	output, e := command.StdoutPipe()
	if e != nil {
		_ = s.set(id, bson.M{"status": "failed", "error": message})
		return
	}
	if e = command.Start(); e != nil {
		_ = s.set(id, bson.M{"status": "failed", "error": "Codex team runtime could not start"})
		return
	}
	scan := bufio.NewScanner(output)
	scan.Buffer(make([]byte, 4096), 262144)
	seq := 0
	for scan.Scan() {
		var event Event
		if e = json.Unmarshal(scan.Bytes(), &event); e != nil {
			continue
		}
		seq++
		if seq > 400 {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			message = "Event budget exhausted"
			break
		}
		event.Seq = seq
		if len(event.Data) > 24576 {
			event.Data, _ = json.Marshal(map[string]interface{}{"truncated": true, "preview": string(event.Data[:16000])})
		}

		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, saveErr := s.Collection.UpdateOne(updateCtx, bson.M{"_id": id}, bson.M{"$push": bson.M{"events": event}, "$set": bson.M{"updatedAt": time.Now().UTC()}})
		cancel()
		if saveErr != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			message = "Event persistence failed; run stopped"
			break
		}
		if event.Type == "run_completed" || event.Type == "run_needs_input" {
			state = "completed"
			if event.Type == "run_needs_input" {
				state = "needs_input"
			}
			var v struct {
				Summary string `json:"summary"`
			}
			_ = json.Unmarshal(event.Data, &v)
			summary = v.Summary
			message = ""
		}
		if event.Type == "run_failed" {
			state = "failed"
			var v struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(event.Data, &v)
			message = v.Error
		}
	}
	e = command.Wait()
	if ctx.Err() != nil {
		state = "cancelled"
		message = "Run cancelled or timed out; no installation executed"
	} else if e != nil && state == "completed" {
		state = "failed"
		message = "Runtime exited unsuccessfully"
	}
	_ = s.set(id, bson.M{"status": state, "summary": summary, "error": message, "updatedAt": time.Now().UTC()})
}
func (s *Service) set(id string, values bson.M) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := s.Collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": values})
	return e
}
func (s *Service) Get(ctx context.Context, id, owner, tenant string, admin bool) (Run, error) {
	filter := bson.M{"_id": id}
	if !admin {
		filter["owner"] = owner
		filter["tenant"] = tenant
	}
	var r Run
	e := s.Collection.FindOne(ctx, filter).Decode(&r)
	return r, e
}
func (s *Service) List(ctx context.Context, owner, tenant string, admin bool) ([]Run, error) {
	filter := bson.M{}
	if !admin {
		filter = bson.M{"owner": owner, "tenant": tenant}
	}
	cur, e := s.Collection.Find(ctx, filter, options.Find().SetSort(bson.M{"createdAt": -1}).SetLimit(30).SetProjection(bson.M{"events": 0}))
	if e != nil {
		return nil, e
	}
	defer cur.Close(ctx)
	out := []Run{}
	e = cur.All(ctx, &out)
	return out, e
}
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != id || s.cancel == nil {
		return fmt.Errorf("run is not active")
	}
	s.cancel()
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
