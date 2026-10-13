// rbac-bootstrap is an explicit local owner initialization; no OS changes.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/free5gc/webconsole/backend/deployment"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Bootstrap failed:", err)
		os.Exit(1)
	}
}
func run() error {
	uri := flag.String("mongo-uri", "mongodb://127.0.0.1:27017", "Mongo URI")
	name := flag.String("database", "free5gc", "Mongo database")
	flag.Parse()
	// Credentials supplied via stdin by the getpass wrapper; never command arguments.
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&in); err != nil {
		return fmt.Errorf("invalid bootstrap input")
	}
	if in.Email == "" || len(in.Password) < 12 || in.Password == "free5gc" {
		return fmt.Errorf("owner name and password of at least 12 characters required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*uri))
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)
	db := client.Database(*name)
	store, err := deployment.NewMongoStore(ctx, db)
	if err != nil {
		return err
	}
	st, err := store.Load(ctx)
	if err != nil {
		return err
	}
	for _, a := range st.Assignments {
		if a.Role == "Admin" {
			return fmt.Errorf("owner already initialized")
		}
	}
	var user struct {
		UserID   string `bson:"userId"`
		TenantID string `bson:"tenantId"`
		Password string `bson:"encryptedPassword"`
	}
	err = db.Collection("userData").FindOne(ctx, bson.M{"email": in.Email}).Decode(&user)
	if err == mongo.ErrNoDocuments {
		hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
		if e != nil {
			return e
		}
		user.UserID = uuid.NewString()
		user.TenantID = uuid.NewString()
		if _, e = db.Collection("tenantData").InsertOne(ctx, bson.M{"tenantId": user.TenantID, "tenantName": "host-owner-" + user.TenantID}); e != nil {
			return e
		}
		if _, e = db.Collection("userData").InsertOne(ctx, bson.M{"userId": user.UserID, "tenantId": user.TenantID, "email": in.Email, "encryptedPassword": string(hash)}); e != nil {
			return e
		}
	} else if err != nil {
		return err
	} else if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("free5gc")) == nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)) != nil {
		return fmt.Errorf("existing owner credentials must match and must not use the default password")
	}
	svc := deployment.Service{Store: store}
	if err = svc.Bootstrap(ctx, user.UserID, user.TenantID); err != nil {
		return err
	}
	fmt.Println("Owner initialized; existing users/subscribers preserved. No privileged worker enabled.")
	return nil
}
