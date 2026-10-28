package testenv

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/apache/rocketmq-client-go/v2/admin"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/redis/go-redis/v9"
)

type Resource struct {
	Kind string
	Name string
}
type Run struct {
	ID        string
	Address   string
	Resources []Resource
	Dir       string `json:"-"`
}

var runID = regexp.MustCompile(`^[a-f0-9]{16}$`)
var suffix = regexp.MustCompile(`^[a-z0-9_]+$`)

func NewRun() (*Run, error) {
	c, e := AdminConfig()
	if e != nil {
		return nil, e
	}
	root, e := Root()
	if e != nil {
		return nil, e
	}
	var id [8]byte
	if _, e = rand.Read(id[:]); e != nil {
		return nil, e
	}
	r := &Run{ID: hex.EncodeToString(id[:]), Address: c.Addr}
	r.Dir = filepath.Join(root, ".tools", "test-runs", r.ID)
	if e = os.MkdirAll(filepath.Dir(r.Dir), 0700); e != nil {
		return nil, e
	}
	if e = os.Mkdir(r.Dir, 0700); e != nil {
		return nil, e
	}
	return r, r.save()
}
func LoadRun(dir string) (*Run, error) {
	data, e := os.ReadFile(filepath.Join(dir, "resources.json"))
	if e != nil {
		return nil, e
	}
	r := new(Run)
	if e = json.Unmarshal(data, r); e != nil {
		return nil, e
	}
	r.Dir = dir
	c, e := AdminConfig()
	if e != nil {
		return nil, e
	}
	if !runID.MatchString(r.ID) || filepath.Base(filepath.Clean(dir)) != r.ID || r.Address != c.Addr {
		return nil, errors.New("resource manifest does not match this run/target")
	}
	for _, v := range r.Resources {
		if !r.valid(v) {
			return nil, errors.New("resource outside recorded run ownership")
		}
	}
	return r, nil
}
func (r *Run) save() error {
	data, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(r.Dir, "resources.tmp"), data, 0600); e != nil {
		return e
	}
	return os.Rename(filepath.Join(r.Dir, "resources.tmp"), filepath.Join(r.Dir, "resources.json"))
}
func (r *Run) Name(kind, label string) string {
	if kind == "retry" {
		return "%RETRY%" + r.Name("group", "consumer")
	}
	if kind == "dlq" {
		return "%DLQ%" + r.Name("group", "consumer")
	}
	if kind == "database" {
		return DatabasePrefix + r.ID + "_" + label
	}
	if kind == "redis" {
		return "dexwallet_integration_" + r.ID + ":" + label
	}
	return "dexwallet_integration_" + r.ID + "_" + label
}
func (r *Run) valid(v Resource) bool {
	if !runID.MatchString(r.ID) {
		return false
	}
	switch v.Kind {
	case "database", "redis", "topic", "group", "retry", "dlq":
	default:
		return false
	}
	if v.Kind == "retry" || v.Kind == "dlq" {
		return v.Name == r.Name(v.Kind, "")
	}
	prefix := r.Name(v.Kind, "")
	return len(v.Name) > len(prefix) && v.Name[:len(prefix)] == prefix && suffix.MatchString(v.Name[len(prefix):])
}

// Record intent before mutation so an interrupted run can recover exact names.
// Names are random per run; callers must not adopt pre-existing resources.
func (r *Run) Record(kind, name string) error {
	v := Resource{kind, name}
	if !r.valid(v) {
		return errors.New("resource outside run ownership")
	}
	for _, x := range r.Resources {
		if x == v {
			return nil
		}
	}
	r.Resources = append(r.Resources, v)
	return r.save()
}
func (r *Run) NewDatabase(ctx context.Context, label string) (string, string, *sql.DB, error) {
	c, e := AdminConfig()
	if e != nil {
		return "", "", nil, e
	}
	db, e := sql.Open("mysql", c.FormatDSN())
	if e != nil {
		return "", "", nil, e
	}
	defer db.Close()
	name := r.Name("database", label)
	if !r.valid(Resource{"database", name}) {
		return "", "", nil, errors.New("invalid database label")
	}
	var count int
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", name).Scan(&count); e != nil {
		return "", "", nil, e
	}
	if count != 0 {
		return "", "", nil, errors.New("refusing to adopt existing schema")
	}
	if e = r.Record("database", name); e != nil {
		return "", "", nil, e
	}
	if _, e = db.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); e != nil {
		return "", "", nil, e
	}
	c.DBName = name
	c.ParseTime = true
	c.MultiStatements = true
	connection, e := sql.Open("mysql", c.FormatDSN())
	return name, c.FormatDSN(), connection, e
}
func (r *Run) Cleanup() error {
	c, e := AdminConfig()
	if e != nil {
		return e
	}
	if r.Address != c.Addr {
		return errors.New("cleanup target mismatch")
	}
	return r.cleanupWith(func(v Resource) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return r.remove(ctx, c.FormatDSN(), v)
	})
}

func (r *Run) cleanupWith(remove func(Resource) error) error {
	var result error
	// Reverse order: group before topics, databases after clients have stopped.
	for i := len(r.Resources) - 1; i >= 0; i-- {
		v := r.Resources[i]
		if !r.valid(v) {
			return errors.New("invalid resource ownership")
		}
		e := remove(v)
		if e != nil {
			result = errors.Join(result, fmt.Errorf("cleanup %s %s: %w", v.Kind, v.Name, e))
			continue
		}
		fmt.Printf("CLEANED %s %s\n", v.Kind, v.Name)
		r.Resources = append(r.Resources[:i], r.Resources[i+1:]...)
		if e = r.save(); e != nil {
			return errors.Join(result, e)
		}
	}
	return result
}
func (r *Run) remove(ctx context.Context, dsn string, v Resource) error {
	switch v.Kind {
	case "database":
		db, e := sql.Open("mysql", dsn)
		if e != nil {
			return e
		}
		defer db.Close()
		_, e = db.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+v.Name+"`")
		return e
	case "redis":
		client := redis.NewClient(&redis.Options{Addr: RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD"), ContextTimeoutEnabled: true})
		defer client.Close()
		return client.Del(ctx, v.Name).Err()
	case "topic", "retry", "dlq":
		a, e := admin.NewAdmin(admin.WithResolver(primitive.NewPassthroughResolver([]string{NameServer})))
		if e != nil {
			return e
		}
		defer a.Close()
		if e = a.DeleteTopic(ctx, admin.WithTopicDelete(v.Name), admin.WithBrokerAddrDelete(BrokerAddress), admin.WithNameSrvAddr([]string{NameServer})); e != nil {
			return e
		}
		topics, e := a.FetchAllTopicList(ctx)
		if e != nil {
			return e
		}
		for _, name := range topics.TopicList {
			if name == v.Name {
				return errors.New("topic still registered after deletion")
			}
		}
		return nil
	case "group":
		root, e := Root()
		if e != nil {
			return e
		}
		cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts", "mqadmin-local.sh"), v.Name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e = cmd.Run(); e != nil {
			return e
		}
		a, e := admin.NewAdmin(admin.WithResolver(primitive.NewPassthroughResolver([]string{NameServer})))
		if e != nil {
			return e
		}
		defer a.Close()
		groups, e := a.GetAllSubscriptionGroup(ctx, BrokerAddress, 5*time.Second)
		if e != nil {
			return e
		}
		if _, exists := groups.SubscriptionGroupTable[v.Name]; exists {
			return errors.New("consumer group still registered after deletion")
		}
		return nil
	}
	return errors.New("unknown resource kind")
}

// Lock prevents recovery cleanup racing a live suite. The OS releases it on exit.
func Lock(dir string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(dir, "run.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("test run is active; refusing concurrent cleanup")
	}
	return f, nil
}
func Recover(dir string) error {
	lock, e := Lock(dir)
	if e != nil {
		return e
	}
	defer lock.Close()
	r, e := LoadRun(dir)
	if e != nil {
		return e
	}
	return r.Cleanup()
}
