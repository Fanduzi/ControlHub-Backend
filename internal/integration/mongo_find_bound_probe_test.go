//go:build integration

// Feasibility probe for a governed MongoDB find worksheet: measures on a real
// mongod which guarantees a bounded find can provide — privileges under a
// find-only role, explain plan shape, maxTimeMS behavior, disconnect handling,
// server-side byte bounding with $bsonSize projections, and traceability.
// Probe only; no product code depends on it.
package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var mongoProbeImages = []struct{ version, image string }{
	{"8.0.38", "mongo:8.0.38@sha256:479e85ced54ffb8a40dade1dcde2da2a9ae858515b9415e9cc54bfb8f0575d01"},
	{"9.0.8", "mongo:9.0.8@sha256:b0c835c26e5997413e985000f1013c275377cd9607761c1784d486f6dd620b7f"},
}

// mongoCountingConn counts bytes read off the socket.
type mongoCountingConn struct {
	net.Conn
	n *atomic.Int64
}

func (c *mongoCountingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.n.Add(int64(n))
	return n, err
}

type mongoCountingDialer struct {
	inner net.Dialer
	n     *atomic.Int64
}

func (d *mongoCountingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.inner.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &mongoCountingConn{Conn: conn, n: d.n}, nil
}

func mongoRandomSecret(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// mongoErrCode extracts a server error code, or -1 for client-side errors.
func mongoErrCode(err error) int64 {
	if err == nil {
		return 0
	}
	var ce mongo.CommandError
	if errors.As(err, &ce) {
		return int64(ce.Code)
	}
	var we mongo.WriteException
	if errors.As(err, &we) {
		if len(we.WriteErrors) > 0 {
			return int64(we.WriteErrors[0].Code)
		}
		if we.WriteConcernError != nil {
			return int64(we.WriteConcernError.Code)
		}
	}
	return -1
}

// mongoDocM normalizes a nested document value (bson.D or bson.M) to bson.M.
// The driver decodes sub-documents inside a bson.M result as bson.D.
func mongoDocM(v any) bson.M {
	switch d := v.(type) {
	case bson.M:
		return d
	case bson.D:
		m := make(bson.M, len(d))
		for _, e := range d {
			m[e.Key] = e.Value
		}
		return m
	}
	return nil
}

// mongoPlanSummary flattens a winningPlan/executionStages chain into a
// readable string of stages, index names, bounds and residual filters.
func mongoPlanSummary(plan bson.M) string {
	var parts []string
	for {
		stage, _ := plan["stage"].(string)
		desc := stage
		if name, ok := plan["indexName"].(string); ok {
			desc += " index=" + name
			if bounds := mongoDocM(plan["indexBounds"]); bounds != nil {
				raw, _ := bson.MarshalExtJSON(bounds, false, false)
				desc += " bounds=" + string(raw)
			}
		}
		if filter := mongoDocM(plan["filter"]); filter != nil {
			raw, _ := bson.MarshalExtJSON(filter, false, false)
			desc += " filter=" + string(raw)
		}
		parts = append(parts, desc)
		next := mongoDocM(plan["inputStage"])
		if next == nil {
			break
		}
		plan = next
	}
	return strings.Join(parts, " -> ")
}

func mongoOpsOn(t *testing.T, ctx context.Context, admin *mongo.Database, ns string) []bson.M {
	t.Helper()
	cur, err := admin.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$currentOp", Value: bson.D{{Key: "allUsers", Value: true}}}},
		{{Key: "$match", Value: bson.D{{Key: "ns", Value: ns}}}},
	})
	if err != nil {
		t.Logf("currentOp error: %v", err)
		return nil
	}
	defer cur.Close(ctx)
	var ops []bson.M
	if err := cur.All(ctx, &ops); err != nil {
		t.Logf("currentOp decode: %v", err)
	}
	return ops
}

func startMongoProbe(t *testing.T, image string) (address string, fixture testcontainers.Container, rootPassword string) {
	t.Helper()
	rootPassword = mongoRandomSecret(t, 12)
	// keyFile is generated inside the container: WithFiles copies files only
	// after the container starts, so an entrypoint cannot depend on them.
	// GLIBC_TUNABLES works around the tcmalloc/rseq startup refusal on Docker
	// Desktop VM kernels 6.19–7.0.13 (SERVER-121912). mongod runs in the
	// foreground so its log (needed by case H) is the container log.
	wrapper := strings.Join([]string{
		"set -e",
		"head -c 600 /dev/urandom | base64 > /data/keyfile",
		"chown mongodb:mongodb /data/keyfile",
		"chmod 0400 /data/keyfile",
		"export GLIBC_TUNABLES=glibc.pthread.rseq=1",
		"gosu mongodb mongod --replSet rs0 --bind_ip_all --keyFile /data/keyfile &",
		"until mongosh --quiet --eval 'db.runCommand({ping:1}).ok' >/dev/null 2>&1; do sleep 0.2; done",
		// rs.initiate before createUser: an uninitiated --replSet member
		// reports not-primary, so writes (createUser) are refused until the
		// set exists and elects itself.
		`mongosh --quiet --eval 'rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]})'`,
		`until mongosh --quiet --eval 'db.hello().isWritablePrimary' 2>/dev/null | grep -qx true; do sleep 0.2; done`,
		`mongosh --quiet --eval 'db.getSiblingDB("admin").createUser({user:"root",pwd:process.env.MONGO_ROOT_PW,roles:[{role:"root",db:"admin"}]})'`,
		"wait",
	}, "\n")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	m, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("27017/tcp"),
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) {
			config.PortBindings = network.PortMap{network.MustParsePort("27017/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1")}}}
			config.Memory = 1 << 30
		}),
		testcontainers.WithEnv(map[string]string{"MONGO_ROOT_PW": rootPassword}),
		testcontainers.WithEntrypoint("bash", "-c"),
		testcontainers.WithCmd(wrapper),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("27017/tcp").SkipInternalCheck()),
	)
	cancel()
	testcontainers.CleanupContainer(t, m)
	if err != nil {
		t.Fatalf("Mongo container %s: %v", image, err)
	}
	port, err := m.MappedPort(t.Context(), "27017/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return "127.0.0.1:" + port.Port(), m, rootPassword
}

func mongoClientFor(t *testing.T, ctx context.Context, address, user, password, authDB string, bytesRead *atomic.Int64) *mongo.Client {
	t.Helper()
	uri := fmt.Sprintf("mongodb://%s:%s@%s/%s?directConnection=true", user, password, address, authDB)
	opts := options.Client().ApplyURI(uri)
	if bytesRead != nil {
		opts.SetDialer(&mongoCountingDialer{n: bytesRead})
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		t.Fatalf("mongo connect %s: %v", user, err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, 60*time.Second)
	defer pingCancel()
	for {
		var hello bson.M
		err := client.Database("admin").RunCommand(pingCtx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello)
		if err == nil {
			if primary, _ := hello["isWritablePrimary"].(bool); primary {
				break
			}
		}
		select {
		case <-pingCtx.Done():
			t.Fatalf("mongo %s never became primary: %v", user, err)
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client
}

func TestMongoFindBoundProbe(t *testing.T) {
	for _, img := range mongoProbeImages {
		t.Run(img.version, func(t *testing.T) {
			probeMongoFind(t, img.image)
		})
	}
}

func probeMongoFind(t *testing.T, image string) {
	address, fixture, rootPassword := startMongoProbe(t, image)
	defer func() {
		if t.Failed() {
			logCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if logs, err := fixture.Logs(logCtx); err == nil {
				defer logs.Close()
				content, _ := io.ReadAll(io.LimitReader(logs, 2<<20))
				lines := strings.Split(string(content), "\n")
				if len(lines) > 40 {
					lines = lines[len(lines)-40:]
				}
				t.Logf("container log tail:\n%s", strings.Join(lines, "\n"))
			}
		}
	}()
	ctx := t.Context()

	root := mongoClientFor(t, ctx, address, "root", rootPassword, "admin", nil)
	admin := root.Database("admin")
	demo := root.Database("demo")
	var serverHello bson.M
	if err := admin.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&serverHello); err != nil {
		t.Fatal(err)
	}
	t.Logf("server: %v image=%s", serverHello["version"], image)

	// ---- Seed (root) ---------------------------------------------------
	seedStart := time.Now()
	orders := demo.Collection("orders")
	for _, idx := range []mongo.IndexModel{
		{Keys: bson.D{{Key: "orderNo", Value: 1}}, Options: options.Index().SetName("orderNo_1").SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}, Options: options.Index().SetName("status_1")},
	} {
		if _, err := orders.Indexes().CreateOne(ctx, idx); err != nil {
			t.Fatal(err)
		}
	}
	statuses := []string{"open", "shipped", "cancelled"}
	const total = 1_000_000
	const batch = 20_000
	docs := make([]any, 0, batch)
	flush := func() {
		if len(docs) == 0 {
			return
		}
		if _, err := orders.InsertMany(ctx, docs); err != nil {
			t.Fatal(err)
		}
		docs = docs[:0]
	}
	for i := 0; i < total; i++ {
		docs = append(docs, bson.D{
			{Key: "orderNo", Value: int64(i)},
			{Key: "status", Value: statuses[i%3]},
			{Key: "region", Value: "cn-east"},
			{Key: "amount", Value: int64(i % 1000)},
			{Key: "note", Value: fmt.Sprintf("order-%d", i)},
			{Key: "customer", Value: bson.D{{Key: "name", Value: fmt.Sprintf("cust-%d", i)}, {Key: "phone", Value: fmt.Sprintf("+86-139-%08d", i)}}},
		})
		if len(docs) == batch {
			flush()
		}
	}
	flush()
	for _, special := range []bson.D{
		{{Key: "orderNo", Value: int64(2_000_000)}, {Key: "status", Value: "open"}, {Key: "region", Value: "cn-north"}, {Key: "amount", Value: 1}, {Key: "note", Value: strings.Repeat("x", 2<<20)}, {Key: "customer", Value: bson.D{{Key: "name", Value: "big-note"}}}},
		{{Key: "orderNo", Value: int64(2_000_001)}, {Key: "status", Value: "open"}, {Key: "region", Value: "cn-north"}, {Key: "amount", Value: 1}, {Key: "note", Value: "tags-doc"}, {Key: "customer", Value: bson.D{{Key: "name", Value: "tagged"}}}, {Key: "tags", Value: makeStringSlice(50_000)}},
		{{Key: "orderNo", Value: int64(2_000_002)}, {Key: "status", Value: "open"}, {Key: "region", Value: "cn-north"}, {Key: "amount", Value: 1}, {Key: "note", Value: "deep-doc"}, {Key: "customer", Value: bson.M{"name": "big-cust", "payload": strings.Repeat("d", 1<<20)}}},
	} {
		if _, err := orders.InsertOne(ctx, special); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := demo.Collection("secrets").InsertOne(ctx, bson.D{{Key: "k", Value: "classified"}}); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d docs + 3 specials in %s", total, time.Since(seedStart).Round(time.Second))

	// ---- Reader role + user (find only on demo.orders) ------------------
	if err := demo.RunCommand(ctx, bson.D{{Key: "createRole", Value: "worksheetReader"}, {Key: "privileges", Value: bson.A{
		bson.D{{Key: "resource", Value: bson.D{{Key: "db", Value: "demo"}, {Key: "collection", Value: "orders"}}}, {Key: "actions", Value: bson.A{"find"}}},
	}}, {Key: "roles", Value: bson.A{}}}).Err(); err != nil {
		t.Fatal(err)
	}
	readerPassword := mongoRandomSecret(t, 12)
	if err := demo.RunCommand(ctx, bson.D{{Key: "createUser", Value: "reader"}, {Key: "pwd", Value: readerPassword},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "worksheetReader"}, {Key: "db", Value: "demo"}}}}}).Err(); err != nil {
		t.Fatal(err)
	}
	var readerBytes atomic.Int64
	reader := mongoClientFor(t, ctx, address, "reader", readerPassword, "demo", &readerBytes)
	rOrders := reader.Database("demo").Collection("orders")

	mark := func() int64 { return readerBytes.Load() }
	delta := func(from int64) int64 { return readerBytes.Load() - from }

	// runFind issues a raw find command (singleBatch/maxTimeMS/comment are
	// wire-level fields the v2 driver's Find options no longer expose) and
	// returns the decoded firstBatch, raw reply, error and wall time.
	runFind := func(coll string, fields bson.D) (docs []bson.M, raw bson.M, err error, wall time.Duration) {
		cmd := append(bson.D{{Key: "find", Value: coll}}, fields...)
		start := time.Now()
		err = reader.Database("demo").RunCommand(ctx, cmd).Decode(&raw)
		wall = time.Since(start)
		if err != nil {
			return nil, raw, err, wall
		}
		if cursor := mongoDocM(raw["cursor"]); cursor != nil {
			if fb, ok := cursor["firstBatch"].(bson.A); ok {
				for _, d := range fb {
					if dm := mongoDocM(d); dm != nil {
						docs = append(docs, dm)
					}
				}
			}
		}
		return docs, raw, nil, wall
	}

	explainOn := func(who *mongo.Client, filter any, verbosity string, extra bson.D) (bson.M, error, time.Duration) {
		cmd := bson.D{{Key: "find", Value: "orders"}, {Key: "filter", Value: filter}}
		cmd = append(cmd, extra...)
		var out bson.M
		start := time.Now()
		err := who.Database("demo").RunCommand(ctx, bson.D{{Key: "explain", Value: cmd}, {Key: "verbosity", Value: verbosity}}).Decode(&out)
		return out, err, time.Since(start)
	}

	// ---- A. Privileges ---------------------------------------------------
	t.Run("A_privileges", func(t *testing.T) {
		_, _, err, _ := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "status", Value: "open"}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}})
		t.Logf("find on orders: err=%v code=%d", err, mongoErrCode(err))

		_, exErr, _ := explainOn(reader, bson.D{{Key: "status", Value: "open"}}, "queryPlanner", nil)
		t.Logf("explain find on orders (reader): err=%v code=%d", exErr, mongoErrCode(exErr))

		for name, do := range map[string]func() error{
			"insert": func() error {
				_, e := rOrders.InsertOne(ctx, bson.D{{Key: "orderNo", Value: int64(9_000_001)}})
				return e
			},
			"update": func() error {
				_, e := rOrders.UpdateOne(ctx, bson.D{{Key: "orderNo", Value: 0}}, bson.D{{Key: "$set", Value: bson.D{{Key: "note", Value: "x"}}}})
				return e
			},
			"delete": func() error { _, e := rOrders.DeleteOne(ctx, bson.D{{Key: "orderNo", Value: 0}}); return e },
			"createIndex": func() error {
				_, e := rOrders.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "region", Value: 1}}})
				return e
			},
			"drop": func() error { return rOrders.Drop(ctx) },
		} {
			err := do()
			t.Logf("%s on orders (reader): code=%d err=%v", name, mongoErrCode(err), err)
		}

		_, _, sErr, _ := runFind("secrets", bson.D{{Key: "filter", Value: bson.D{}}})
		t.Logf("find on secrets (reader): code=%d err=%v", mongoErrCode(sErr), sErr)

		for name, pipeline := range map[string]mongo.Pipeline{
			"aggregate_match": {{{Key: "$match", Value: bson.D{{Key: "status", Value: "open"}}}}},
			"aggregate_out":   {{{Key: "$match", Value: bson.D{{Key: "status", Value: "open"}}}}, {{Key: "$out", Value: "probe_out"}}},
			"aggregate_merge": {{{Key: "$match", Value: bson.D{{Key: "status", Value: "open"}}}}, {{Key: "$merge", Value: bson.D{{Key: "into", Value: "probe_merge"}}}}},
		} {
			aggCur, err := rOrders.Aggregate(ctx, pipeline)
			if err == nil {
				var tmp []bson.M
				err = aggCur.All(ctx, &tmp)
				aggCur.Close(ctx)
			}
			t.Logf("%s (reader): code=%d err=%v", name, mongoErrCode(err), err)
		}

		for name, item := range map[string]struct {
			db  string
			cmd bson.D
		}{
			"listCollections": {"demo", bson.D{{Key: "listCollections", Value: 1}, {Key: "nameOnly", Value: true}}},
			"listDatabases":   {"admin", bson.D{{Key: "listDatabases", Value: 1}}},
			"currentOp":       {"admin", bson.D{{Key: "currentOp", Value: 1}}},
			"serverStatus":    {"admin", bson.D{{Key: "serverStatus", Value: 1}}},
		} {
			err := reader.Database(item.db).RunCommand(ctx, item.cmd).Err()
			t.Logf("%s on %s (reader): code=%d err=%v", name, item.db, mongoErrCode(err), err)
		}

		_, _, err, _ = runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "$where", Value: "sleep(100)||true"}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}})
		t.Logf("$where sleep(100)||true (reader): code=%d err=%v", mongoErrCode(err), err)
		functionFilter := bson.D{{Key: "status", Value: bson.D{{Key: "$eq", Value: bson.D{{Key: "$function", Value: bson.D{
			{Key: "body", Value: "function(){return 'open'}"}, {Key: "args", Value: bson.A{}}, {Key: "lang", Value: "js"},
		}}}}}}}
		_, _, err, _ = runFind("orders", bson.D{{Key: "filter", Value: functionFilter}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}})
		t.Logf("$function (reader): code=%d err=%v", mongoErrCode(err), err)
	})

	// ---- B. Explain classification ---------------------------------------
	t.Run("B_explain", func(t *testing.T) {
		filters := []struct {
			name   string
			filter bson.D
		}{
			{"status_eq", bson.D{{Key: "status", Value: "open"}}},
			{"region_eq", bson.D{{Key: "region", Value: "cn-east"}}},
			{"empty", bson.D{}},
			{"orderNo_gte0", bson.D{{Key: "orderNo", Value: bson.D{{Key: "$gte", Value: 0}}}}},
			{"orderNo_range", bson.D{{Key: "orderNo", Value: bson.D{{Key: "$gte", Value: 500000}, {Key: "$lt", Value: 500050}}}}},
			{"status_region", bson.D{{Key: "status", Value: "open"}, {Key: "region", Value: "x"}}},
			{"status_in", bson.D{{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"open", "shipped"}}}}}},
			{"or_mixed", bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "status", Value: "open"}}, bson.D{{Key: "region", Value: "x"}}}}}},
		}
		for _, f := range filters {
			out, err, wall := explainOn(reader, f.filter, "queryPlanner", nil)
			if err != nil {
				t.Logf("%-14s explain err=%v code=%d", f.name, err, mongoErrCode(err))
				continue
			}
			qp := mongoDocM(out["queryPlanner"])
			winning := mongoDocM(qp["winningPlan"])
			rejected, hasRejected := qp["rejectedPlans"].(bson.A)
			t.Logf("%-14s explain=%s plan=%s rejected=%d(%v)", f.name, wall.Round(time.Millisecond), mongoPlanSummary(winning), len(rejected), hasRejected)
		}
	})

	// ---- C. Hint: "uses an index" can still scan ~333k docs --------------
	t.Run("C_hint", func(t *testing.T) {
		execStats := func(name string, filter bson.D, hint any) {
			out, err, _ := explainOn(root, filter, "executionStats", bson.D{{Key: "hint", Value: hint}})
			if err != nil {
				t.Logf("%s hint=%v explain err=%v code=%d", name, hint, err, mongoErrCode(err))
				return
			}
			es := mongoDocM(out["executionStats"])
			t.Logf("%s hint=%v keysExamined=%v docsExamined=%v nReturned=%v timeMs=%v stages=%s",
				name, hint, es["totalKeysExamined"], es["totalDocsExamined"], es["nReturned"], es["executionTimeMillis"],
				mongoPlanSummary(mongoDocM(es["executionStages"])))
		}
		execStats("region_eq", bson.D{{Key: "region", Value: "cn-east"}}, "status_1")
		execStats("status+region", bson.D{{Key: "status", Value: "open"}, {Key: "region", Value: "nope"}}, "status_1")
		execStats("missing_index", bson.D{{Key: "status", Value: "open"}}, "nope_1")
	})

	// ---- D. maxTimeMS ------------------------------------------------------
	t.Run("D_maxTimeMS", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			filter bson.D
			hint   any
		}{
			{"collscan_nope", bson.D{{Key: "region", Value: "nope"}}, nil},
			{"hint_scan", bson.D{{Key: "status", Value: "open"}, {Key: "region", Value: "nope"}}, "status_1"},
		} {
			fields := bson.D{{Key: "filter", Value: c.filter}, {Key: "limit", Value: 50}, {Key: "singleBatch", Value: true}, {Key: "maxTimeMS", Value: 100}}
			if c.hint != nil {
				fields = append(fields, bson.E{Key: "hint", Value: c.hint})
			}
			_, _, err, wall := runFind("orders", fields)
			t.Logf("%s maxTimeMS=100: code=%d err=%v wall=%s", c.name, mongoErrCode(err), err, wall)
			if mongoErrCode(err) != 50 {
				t.Fatalf("%s: expected MaxTimeMSExpired (50), got code=%d err=%v", c.name, mongoErrCode(err), err)
			}
			if wall > time.Second {
				t.Fatalf("%s: client wall time %s > 1s", c.name, wall)
			}
		}
		deadline := time.Now().Add(500 * time.Millisecond)
		for {
			if len(mongoOpsOn(t, ctx, admin, "demo.orders")) == 0 {
				t.Logf("currentOp ns=demo.orders empty within 500ms after MaxTimeMSExpired")
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("server op still running 500ms after MaxTimeMSExpired")
			}
			time.Sleep(25 * time.Millisecond)
		}
	})

	// ---- E. Client cancel without maxTimeMS ------------------------------
	t.Run("E_cancel", func(t *testing.T) {
		opCtx, cancelOp := context.WithCancel(ctx)
		start := time.Now()
		done := make(chan error, 1)
		go func() {
			done <- reader.Database("demo").RunCommand(opCtx, bson.D{
				{Key: "find", Value: "orders"}, {Key: "filter", Value: bson.D{{Key: "region", Value: "nope"}}}, {Key: "limit", Value: 50}, {Key: "singleBatch", Value: true},
			}).Err()
		}()
		time.Sleep(50 * time.Millisecond)
		cancelOp()
		err := <-done
		t.Logf("cancelled find: err=%v code=%d after=%s", err, mongoErrCode(err), time.Since(start).Round(time.Millisecond))

		goneAt := time.Duration(-1)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if len(mongoOpsOn(t, ctx, admin, "demo.orders")) == 0 {
				goneAt = time.Since(start)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Logf("server op after client cancel: goneAt=%s", goneAt.Round(time.Millisecond))
	})

	// ---- F. Byte bounds ----------------------------------------------------
	t.Run("F_byteBounds", func(t *testing.T) {
		boundedProjection := func(field string) bson.D {
			return bson.D{
				{Key: "orderNo", Value: 1},
				{Key: field, Value: bson.D{{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$lte", Value: bson.A{bson.D{{Key: "$bsonSize", Value: bson.D{{Key: "v", Value: "$" + field}}}}, 1024}}},
					"$" + field,
					"$$REMOVE",
				}}}},
				{Key: field + "_over", Value: bson.D{{Key: "$gt", Value: bson.A{bson.D{{Key: "$bsonSize", Value: bson.D{{Key: "v", Value: "$" + field}}}}, 1024}}}},
			}
		}
		for _, c := range []struct {
			name    string
			orderNo int64
			field   string
		}{
			{"note_2MiB", 2_000_000, "note"},
			{"tags_50k", 2_000_001, "tags"},
			{"customer_1MiB", 2_000_002, "customer"},
		} {
			b := mark()
			docs, _, err, wall := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "orderNo", Value: c.orderNo}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}})
			t.Logf("%-14s plain find: docs=%d code=%d bytes=%d wall=%s", c.name, len(docs), mongoErrCode(err), delta(b), wall.Round(time.Millisecond))
			if c.name == "note_2MiB" && delta(b) < 1<<20 {
				t.Fatalf("note_2MiB plain find read only %d bytes", delta(b))
			}

			b = mark()
			docs, _, err, wall = runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "orderNo", Value: c.orderNo}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}, {Key: "projection", Value: boundedProjection(c.field)}})
			t.Logf("%-14s bounded projection: docs=%d code=%d bytes=%d wall=%s doc=%v", c.name, len(docs), mongoErrCode(err), delta(b), wall.Round(time.Millisecond), docs)
			if err != nil {
				t.Fatalf("%s bounded projection err=%v code=%d", c.name, err, mongoErrCode(err))
			}
			if delta(b) > 16<<10 {
				t.Fatalf("%s bounded projection read %d bytes > 16KiB", c.name, delta(b))
			}
			if len(docs) == 1 && docs[0][c.field+"_over"] != true {
				t.Fatalf("%s expected %s_over=true, got doc=%v", c.name, c.field, docs[0])
			}
		}

		b := mark()
		docs, _, err, _ := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "orderNo", Value: 5}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}, {Key: "projection", Value: boundedProjection("missing")}})
		t.Logf("missing-field projection: docs=%d code=%d bytes=%d doc=%v keys=%v", len(docs), mongoErrCode(err), delta(b), docs, keysOf(docs[0]))

		b = mark()
		docs, _, err, _ = runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "orderNo", Value: 5}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}, {Key: "projection", Value: bson.D{
			{Key: "orderNo", Value: 1},
			{Key: "_id_size", Value: bson.D{{Key: "$bsonSize", Value: bson.D{{Key: "v", Value: "$_id"}}}}},
		}}})
		t.Logf("_id size projection: docs=%d code=%d bytes=%d doc=%v", len(docs), mongoErrCode(err), delta(b), docs)

		b = mark()
		docs, _, err, wall := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "orderNo", Value: bson.D{{Key: "$gte", Value: 0}, {Key: "$lt", Value: 1000}}}}},
			{Key: "singleBatch", Value: true}, {Key: "projection", Value: bson.D{
				{Key: "orderNo", Value: 1},
				{Key: "note_over", Value: bson.D{{Key: "$gt", Value: bson.A{bson.D{{Key: "$bsonSize", Value: bson.D{{Key: "v", Value: "$note"}}}}, 1024}}}},
			}}})
		t.Logf("$bsonSize on 1000 normal docs: docs=%d code=%d bytes=%d wall=%s", len(docs), mongoErrCode(err), delta(b), wall.Round(time.Millisecond))
	})

	// ---- G. Defaults / backstops -------------------------------------------
	t.Run("G_backstops", func(t *testing.T) {
		err := admin.RunCommand(ctx, bson.D{{Key: "setClusterParameter", Value: bson.D{
			{Key: "defaultMaxTimeMS", Value: bson.D{{Key: "readOperations", Value: 300}}},
		}}}).Err()
		t.Logf("setClusterParameter defaultMaxTimeMS readOperations=300: err=%v code=%d", err, mongoErrCode(err))
		if err == nil {
			_, _, ferr, wall := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "region", Value: "nope"}}}, {Key: "limit", Value: 50}, {Key: "singleBatch", Value: true}})
			t.Logf("reader find under defaultMaxTimeMS: code=%d err=%v wall=%s", mongoErrCode(ferr), ferr, wall.Round(time.Millisecond))
			rerr := admin.RunCommand(ctx, bson.D{{Key: "setClusterParameter", Value: bson.D{{Key: "defaultMaxTimeMS", Value: bson.D{}}}}}).Err()
			t.Logf("reset defaultMaxTimeMS via empty doc: err=%v code=%d", rerr, mongoErrCode(rerr))
			var got bson.M
			gerr := admin.RunCommand(ctx, bson.D{{Key: "getClusterParameter", Value: "defaultMaxTimeMS"}}).Decode(&got)
			t.Logf("getClusterParameter defaultMaxTimeMS after reset: %v err=%v", got["clusterParameter"], gerr)
		}

		var param bson.M
		err = admin.RunCommand(ctx, bson.D{{Key: "getParameter", Value: 1}, {Key: "notablescan", Value: 1}}).Decode(&param)
		t.Logf("getParameter notablescan: %v err=%v", param, err)
		err = admin.RunCommand(ctx, bson.D{{Key: "setParameter", Value: 1}, {Key: "notablescan", Value: true}}).Err()
		t.Logf("setParameter notablescan=true: err=%v code=%d", err, mongoErrCode(err))
		if err == nil {
			_, _, ferr, _ := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "region", Value: "nope"}}}, {Key: "limit", Value: 1}, {Key: "singleBatch", Value: true}})
			t.Logf("reader COLLSCAN under notablescan: code=%d err=%v", mongoErrCode(ferr), ferr)
			err = admin.RunCommand(ctx, bson.D{{Key: "setParameter", Value: 1}, {Key: "notablescan", Value: false}}).Err()
			t.Logf("setParameter notablescan=false: err=%v", err)
		}
	})

	// ---- H. Traceability ----------------------------------------------------
	t.Run("H_traceability", func(t *testing.T) {
		const marker = "controlhub:probe-123"
		if err := demo.RunCommand(ctx, bson.D{{Key: "profile", Value: 0}, {Key: "slowms", Value: 50}}).Err(); err != nil {
			t.Logf("setProfilingLevel(0,{slowms:50}): %v", err)
		}
		done := make(chan error, 1)
		go func() {
			done <- reader.Database("demo").RunCommand(ctx, bson.D{
				{Key: "find", Value: "orders"}, {Key: "filter", Value: bson.D{{Key: "region", Value: "nope"}}},
				{Key: "limit", Value: 50}, {Key: "singleBatch", Value: true}, {Key: "maxTimeMS", Value: 1000}, {Key: "comment", Value: marker},
			}).Err()
		}()
		time.Sleep(200 * time.Millisecond)
		ops := mongoOpsOn(t, ctx, admin, "demo.orders")
		foundComment := false
		for _, op := range ops {
			raw, _ := bson.MarshalExtJSON(op, false, false)
			if strings.Contains(string(raw), marker) {
				foundComment = true
			}
			t.Logf("currentOp during marked find: %s", truncate(string(raw), 400))
		}
		t.Logf("currentOp exposes comment marker: %v (ops seen: %d)", foundComment, len(ops))
		err := <-done
		t.Logf("marked find finished: code=%d err=%v", mongoErrCode(err), err)

		logCtx, done2 := context.WithTimeout(ctx, 10*time.Second)
		defer done2()
		logs, lerr := fixture.Logs(logCtx)
		if lerr == nil {
			defer logs.Close()
			content, _ := io.ReadAll(io.LimitReader(logs, 8<<20))
			inLog := strings.Contains(string(content), marker)
			t.Logf("mongod log contains comment marker: %v", inLog)
			for _, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, marker) {
					t.Logf("log line: %s", truncate(line, 500))
					break
				}
			}
		} else {
			t.Logf("container logs: %v", lerr)
		}
	})

	// ---- I. Response shape ----------------------------------------------------
	t.Run("I_cursorShape", func(t *testing.T) {
		b := mark()
		_, raw, err, _ := runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "status", Value: "open"}}}, {Key: "limit", Value: 10}, {Key: "singleBatch", Value: true}})
		var cursorID any
		if c := mongoDocM(raw["cursor"]); c != nil {
			cursorID = c["id"]
		}
		t.Logf("singleBatch find: cursor.id=%v err=%v bytes=%d", cursorID, err, delta(b))
		_, raw, err, _ = runFind("orders", bson.D{{Key: "filter", Value: bson.D{{Key: "status", Value: "open"}}}, {Key: "limit", Value: 10}})
		cursorID = nil
		if c := mongoDocM(raw["cursor"]); c != nil {
			cursorID = c["id"]
		}
		t.Logf("no-singleBatch limit=10 find: cursor.id=%v err=%v", cursorID, err)
	})
}

func makeStringSlice(n int) bson.A {
	a := make(bson.A, n)
	for i := range a {
		a[i] = fmt.Sprintf("tag-%d", i)
	}
	return a
}

func keysOf(m bson.M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
