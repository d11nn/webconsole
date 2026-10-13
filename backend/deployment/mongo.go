package deployment

import (
	"context"
	"encoding/json"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
	"sync"
)

// One versioned Mongo document makes approval + queue + audit atomic even on
// standalone MongoDB. Separate from subscriber collections; no replica required.
type MongoStore struct{ Collection *mongo.Collection }
type document struct {
	ID       string `bson:"_id"`
	Revision int64  `bson:"revision"`
	Payload  []byte `bson:"payload"`
}

func NewMongoStore(ctx context.Context, db *mongo.Database) (*MongoStore, error) {
	c := db.Collection("deploymentAuthorization", options.Collection().SetWriteConcern(writeconcern.Majority()))
	b, _ := json.Marshal(Empty())
	_, e := c.UpdateOne(ctx, bson.M{"_id": "host"}, bson.M{"$setOnInsert": document{"host", 0, b}}, options.Update().SetUpsert(true))
	return &MongoStore{c}, e
}
func (m *MongoStore) Load(ctx context.Context) (State, error) {
	var d document
	if e := m.Collection.FindOne(ctx, bson.M{"_id": "host"}).Decode(&d); e != nil {
		return State{}, e
	}
	var st State
	e := json.Unmarshal(d.Payload, &st)
	return st, e
}
func (m *MongoStore) Save(ctx context.Context, rev int64, st State) error {
	b, e := json.Marshal(st)
	if e != nil {
		return e
	}
	r, e := m.Collection.ReplaceOne(ctx, bson.M{"_id": "host", "revision": rev}, document{"host", st.Revision, b})
	if e != nil {
		return e
	}
	if r.MatchedCount != 1 {
		return ErrConflict
	}
	return nil
}

// MemoryStore is exclusively for isolated mock tests/demo, never production.
type MemoryStore struct {
	mu    sync.Mutex
	state State
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{state: Empty()} }
func clone(st State) State {
	b, _ := json.Marshal(st)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (m *MemoryStore) Load(ctx context.Context) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return State{}, e
	}
	return clone(m.state), nil
}
func (m *MemoryStore) Save(ctx context.Context, rev int64, st State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if m.state.Revision != rev {
		return ErrConflict
	}
	m.state = clone(st)
	return nil
}
