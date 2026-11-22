//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/serverboot"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoWorksheetHTTP(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()
	address, rootPassword, caPEM, tlsConfig := startMongoWorksheetFixture(t)
	readerPassword := mongoRandomSecret(t, 12)

	root := mongoFixtureClient(t, address, "root", rootPassword, "admin", tlsConfig)
	admin := root.Database("admin")
	demo := root.Database("demo")
	orders := demo.Collection("orders")

	// Custom role: find on demo.orders only, mirroring the product docs.
	var roleOut bson.M
	if err := demo.RunCommand(ctx, bson.D{
		{Key: "createRole", Value: "worksheetReader"},
		{Key: "privileges", Value: bson.A{bson.D{
			{Key: "resource", Value: bson.D{{Key: "db", Value: "demo"}, {Key: "collection", Value: "orders"}}},
			{Key: "actions", Value: bson.A{"find"}},
		}}},
		{Key: "roles", Value: bson.A{}},
	}).Decode(&roleOut); err != nil {
		t.Fatalf("create worksheetReader role: %v", err)
	}
	if err := demo.RunCommand(ctx, bson.D{
		{Key: "createUser", Value: "reader"},
		{Key: "pwd", Value: readerPassword},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "worksheetReader"}, {Key: "db", Value: "demo"}}}},
	}).Decode(&roleOut); err != nil {
		t.Fatalf("create reader: %v", err)
	}
	reader := mongoFixtureClient(t, address, "reader", readerPassword, "demo", tlsConfig)
	// The find-only role must not write, cross collections, or aggregate-write.
	for _, command := range []bson.D{
		{{Key: "insert", Value: "orders"}, {Key: "documents", Value: bson.A{bson.D{{Key: "x", Value: 1}}}}},
		{{Key: "find", Value: "secrets"}, {Key: "filter", Value: bson.D{}}},
		{{Key: "aggregate", Value: "orders"}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$out", Value: "o2"}}}}, {Key: "cursor", Value: bson.D{}}},
	} {
		if err := reader.Database("demo").RunCommand(ctx, command).Err(); mongoErrCode(err) != 13 {
			t.Fatalf("reader role allowed %v: %v", command[0].Value, err)
		}
	}

	// Seed: 300k ordinary docs, one 2 MiB-note doc, one known _id doc, and an
	// unapproved collection.
	seedStart := time.Now()
	for _, idx := range []mongo.IndexModel{
		{Keys: bson.D{{Key: "orderNo", Value: 1}}, Options: options.Index().SetName("orderNo_1").SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}, Options: options.Index().SetName("status_1")},
	} {
		if _, err := orders.Indexes().CreateOne(ctx, idx); err != nil {
			t.Fatal(err)
		}
	}
	statuses := []string{"open", "shipped", "cancelled"}
	const total, batch = 300_000, 20_000
	docs := make([]any, 0, batch)
	for i := 0; i < total; i++ {
		docs = append(docs, bson.D{
			{Key: "orderNo", Value: int64(i)},
			{Key: "status", Value: statuses[i%3]},
			{Key: "region", Value: "cn-east"},
			{Key: "note", Value: fmt.Sprintf("order-%d", i)},
			{Key: "customer", Value: bson.D{{Key: "name", Value: fmt.Sprintf("cust-%d", i)}}},
			{Key: "cost", Value: int64(i)},
		})
		if len(docs) == batch {
			if _, err := orders.InsertMany(ctx, docs); err != nil {
				t.Fatal(err)
			}
			docs = docs[:0]
		}
	}
	knownID, err := bson.ObjectIDFromHex("64b5f0aabbccddee00112233")
	if err != nil {
		t.Fatal(err)
	}
	for _, special := range []bson.D{
		{{Key: "orderNo", Value: int64(2_000_000)}, {Key: "status", Value: "open"}, {Key: "region", Value: "cn-north"}, {Key: "note", Value: strings.Repeat("x", 2<<20)}, {Key: "customer", Value: bson.D{{Key: "name", Value: "big-note"}}}},
		{{Key: "_id", Value: knownID}, {Key: "orderNo", Value: int64(2_000_001)}, {Key: "status", Value: "open"}, {Key: "region", Value: "cn-north"}, {Key: "note", Value: "by-id"}, {Key: "customer", Value: bson.D{{Key: "name", Value: "id-doc"}}}},
	} {
		if _, err := orders.InsertOne(ctx, special); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := demo.Collection("secrets").InsertOne(ctx, bson.D{{Key: "k", Value: "classified"}}); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d documents in %s", total, time.Since(seedStart).Round(time.Millisecond))

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)
	resources := mysql.NewResourceRepository(db)
	target, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mongodb", Name: "mongo-worksheet-fixture", DisplayName: "MongoDB Worksheet Fixture",
		EnvironmentID: envStaging, OwnerID: ownerDBA, LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual", Labels: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.UpsertDatabaseInstanceProfile(ctx, target.ID, "mongodb", "8.0.38", host, port, "primary"); err != nil {
		t.Fatal(err)
	}
	const email = "mongo-worksheet-editor@example.com"
	user := insertAuthzTestUser(t, db, email, "editor")
	other := insertAuthzTestUser(t, db, "mongo-worksheet-other@example.com", "editor")
	administrator := insertAuthzTestUser(t, db, "mongo-worksheet-admin@example.com", "admin")
	t.Cleanup(func() {
		for _, id := range []uint64{user, other, administrator} {
			deleteAuthzTestUser(t, db, id)
		}
	})

	raw := model.ResultDisclosureRawCopyAllowed
	collections := map[string]map[string]model.ResultDisclosureMode{
		"orders": {"_id": raw, "orderNo": raw, "status": raw, "region": raw, "note": raw, "customer": model.ResultDisclosureMaskedNoCopy},
	}
	manifestPath := filepath.Join(t.TempDir(), "active.json")
	manifest := map[string]any{
		"targetId": target.ID, "environment": "Staging", "address": address, "user": "reader",
		"credentialRef": "MONGO_WORKSHEET_FIXTURE", "caPem": caPEM, "users": []uint64{user},
		"database": "demo", "collections": collections,
		"limits": map[string]any{"maxConcurrentRuns": 16, "maxConcurrentRunsPerUser": 4, "runsPerSecond": 50, "runsPerMinutePerUser": 600},
	}
	publish := func() error {
		contents, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		next := manifestPath + ".next"
		if err := os.WriteFile(next, contents, 0600); err != nil {
			return err
		}
		return os.Rename(next, manifestPath)
	}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	dsn := url.URL{Scheme: "mongodb", Host: address, User: url.UserPassword("reader", readerPassword), Path: "/demo"}
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_MONGO_WORKSHEET_FIXTURE", dsn.String())
	t.Setenv("CONTROLHUB_MONGO_WORKSHEET_CONFIG", manifestPath)
	t.Setenv("CONTROLHUB_ENVIRONMENT", "nonproduction")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	deps, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret, MongoWorksheetPath: cfg.MongoWorksheetPath})
	h := api.NewRouter(deps)
	bearer := mustLogin(t, h, email, "secret123")
	otherBearer := mustLogin(t, h, "mongo-worksheet-other@example.com", "secret123")
	adminBearer := mustLogin(t, h, "mongo-worksheet-admin@example.com", "secret123")
	path := fmt.Sprintf("/query-targets/%d", target.ID)
	const cred = `{"credentialRef":"MONGO_WORKSHEET_FIXTURE","enabled":true,"environmentPolicy":"non_prod_only"}`
	put := func(body string, status int) {
		t.Helper()
		r := doBearerWithBody(t, h, "PUT", path+"/credential", adminBearer, body)
		if r.Code != status {
			t.Fatalf("credential setup %d, want %d: %s", r.Code, status, r.Body)
		}
	}
	put(strings.Replace(cred, "non_prod_only", "all_environments", 1), 400)
	put(cred, 200)

	assertTarget := func(handler http.Handler, token string, run bool) {
		t.Helper()
		r := doBearer(t, handler, "GET", fmt.Sprintf("/query-targets?targetId=%d", target.ID), token)
		var list model.QueryTargetListResponse
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
			t.Fatalf("MongoDB target discovery failed: %d %s", r.Code, r.Body)
		}
		item := list.Items[0]
		a := item.AvailableActions
		if a.Run != run || a.Export || a.Explain || a.SaveSheet || item.Capability.EditorMode != "mongo" {
			t.Fatalf("unexpected MongoDB target actions: %+v mode=%s", a, item.Capability.EditorMode)
		}
		if !run {
			if len(item.SchemaPreview) != 0 {
				t.Fatal("MongoDB object list did not honor approval")
			}
			return
		}
		if len(item.SchemaPreview) != 1 || item.SchemaPreview[0].Kind != "mongo_collection" || item.SchemaPreview[0].Name != "orders" {
			t.Fatalf("unexpected MongoDB object list: %+v", item.SchemaPreview)
		}
		var fields []string
		for _, child := range item.SchemaPreview[0].Children {
			if child.Kind != "mongo_field" {
				t.Fatalf("unexpected MongoDB child kind: %+v", child)
			}
			fields = append(fields, child.Name)
		}
		want := []string{"_id", "customer", "note", "orderNo", "region", "status"}
		if fmt.Sprint(fields) != fmt.Sprint(want) {
			t.Fatalf("MongoDB field list: %v", fields)
		}
	}
	assertTarget(h, bearer, true)
	assertTarget(h, otherBearer, false)
	off, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret})
	closed := api.NewRouter(off)
	assertTarget(closed, bearer, false)

	run := func(handler http.Handler, token string, request model.QueryExecuteRequest, status int, code string) model.QueryExecuteResponse {
		t.Helper()
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		r := doBearerWithBody(t, handler, "POST", path+"/execute", token, string(body))
		if r.Code != status {
			t.Fatalf("MongoDB Run %d, want %d: %s", r.Code, status, r.Body)
		}
		var out model.QueryExecuteResponse
		if status == 200 {
			if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil || out.ExecutionID == 0 || out.Engine != "mongodb" || out.Pagination != nil {
				t.Fatalf("unexpected MongoDB result contract: %s", r.Body)
			}
			if len(out.CellTruncated) != len(out.Rows) {
				t.Fatalf("cellTruncated shape mismatch: %s", r.Body)
			}
		} else if !strings.Contains(r.Body.String(), `"error":"`+code+`"`) || strings.Contains(r.Body.String(), `"rows"`) ||
			strings.Contains(r.Body.String(), "orders") || strings.Contains(r.Body.String(), "cn-east") {
			t.Fatalf("unexpected MongoDB error contract: %s", r.Body)
		}
		return out
	}
	statement := func(s string) model.QueryExecuteRequest { return model.QueryExecuteRequest{Statement: s} }

	run(closed, bearer, statement(`db.orders.find({"status":"open"})`), 403, "query_not_allowed")
	run(h, otherBearer, statement(`db.orders.find({"status":"open"})`), 403, "query_not_allowed")

	columnIndex := func(out model.QueryExecuteResponse, name string) int {
		t.Helper()
		for i, c := range out.Columns {
			if c.Name == name {
				return i
			}
		}
		t.Fatalf("column %s missing in %v", name, out.Columns)
		return -1
	}
	open := run(h, bearer, statement(`db.orders.find({"status": "open"})`), 200, "")
	if len(open.Rows) == 0 || len(open.Rows) > 50 {
		t.Fatalf("open find rows: %d", len(open.Rows))
	}
	statusCol := columnIndex(open, "status")
	for _, row := range open.Rows {
		if row[statusCol] != "open" {
			t.Fatalf("open find returned %v", row[statusCol])
		}
	}
	shipped := run(h, bearer, statement(`db.orders.find({"status": "shipped"})`), 200, "")
	orderNoCol := columnIndex(open, "orderNo")
	if open.Rows[0][orderNoCol] == shipped.Rows[0][columnIndex(shipped, "orderNo")] {
		t.Fatal("open and shipped finds returned the same first row")
	}
	if len(open.Columns) != 6 || open.Columns[0].Name != "_id" {
		t.Fatalf("default columns: %v", open.Columns)
	}
	projected := run(h, bearer, statement(`db.orders.find({"status": "open"}, {"orderNo": 1, "status": 1})`), 200, "")
	if len(projected.Columns) != 2 || projected.Columns[0].Name != "orderNo" || projected.Columns[1].Name != "status" {
		t.Fatalf("projection columns: %v", projected.Columns)
	}
	ranged := run(h, bearer, statement(`db.orders.find({"orderNo": {"$gte": 100, "$lt": 105}})`), 200, "")
	if len(ranged.Rows) != 5 {
		t.Fatalf("range find rows: %d", len(ranged.Rows))
	}
	in := run(h, bearer, statement(`db.orders.find({"status": {"$in": ["open", "shipped"]}})`), 200, "")
	inStatus := columnIndex(in, "status")
	for _, row := range in.Rows {
		if row[inStatus] != "open" && row[inStatus] != "shipped" {
			t.Fatalf("$in find returned %v", row[inStatus])
		}
	}
	byID := run(h, bearer, statement(`db.orders.find({"_id": {"$oid": "64b5f0aabbccddee00112233"}})`), 200, "")
	if len(byID.Rows) != 1 || byID.Rows[0][columnIndex(byID, "_id")] != "64b5f0aabbccddee00112233" {
		t.Fatalf("_id find: %+v", byID.Rows)
	}
	if open.LimitApplied != 50 {
		t.Fatalf("default limitApplied: %d", open.LimitApplied)
	}
	limited := run(h, bearer, model.QueryExecuteRequest{Statement: `db.orders.find({"status": "open"})`, MaxRows: 5}, 200, "")
	if len(limited.Rows) != 5 || !limited.Truncated || limited.LimitApplied != 5 {
		t.Fatalf("limited find: rows=%d truncated=%v limitApplied=%d", len(limited.Rows), limited.Truncated, limited.LimitApplied)
	}
	capped := run(h, bearer, model.QueryExecuteRequest{Statement: `db.orders.find({"status": "open"})`, MaxRows: 500}, 200, "")
	if capped.LimitApplied != 100 {
		t.Fatalf("clamped limitApplied: %d", capped.LimitApplied)
	}
	big := run(h, bearer, statement(`db.orders.find({"orderNo": 2000000})`), 200, "")
	if len(big.Rows) != 1 {
		t.Fatalf("2MiB doc rows: %d", len(big.Rows))
	}
	noteCol := columnIndex(big, "note")
	row, marks := big.Rows[0], big.CellTruncated[0]
	if !marks[noteCol] || row[noteCol] != nil || row[columnIndex(big, "orderNo")] == nil {
		t.Fatalf("2MiB note cell not bounded: %v", row)
	}
	customerCol := columnIndex(big, "customer")
	if row[customerCol] != "[MASKED]" || big.Columns[customerCol].CopyAllowed {
		t.Fatalf("masked customer cell: %v copy=%v", row[customerCol], big.Columns[customerCol].CopyAllowed)
	}

	// Index gate: approved fields that cannot plan through an index are refused
	// before the find runs.
	for _, s := range []string{`db.orders.find({"region": "cn-east"})`, `db.orders.find({})`} {
		run(h, bearer, statement(s), 400, "query_index_required")
	}
	for _, s := range []string{
		`db.orders.find({"$or": [{"status": "open"}, {"region": "x"}]})`,
		`db.orders.find({"$where": "sleep(100)||true"})`,
		`db.orders.find({"status": {"$function": "f()"}})`,
		`db.orders.find({"$expr": {"$eq": ["$a", 1]}})`,
		`db.orders.find({"status": {"$regex": "^o"}})`,
		`db.orders.aggregate([{"$match": {"status": "open"}}])`,
		`db.orders.find({"status": "open"}).limit(1)`,
		`db.orders.find({"status": "open"}, {"orderNo": 0})`,
	} {
		run(h, bearer, statement(s), 400, "validation_failed")
	}
	for _, s := range []string{
		`db.secrets.find({})`,
		`db.orders.find({"cost": 5})`,
		`db.orders.find({"status": "open"}, {"cost": 1})`,
		// customer is approved masked_no_copy: filtering on it would confirm
		// guessed values by equality, so the filter is disclosure-blocked.
		`db.orders.find({"customer": "x"})`,
		`db.orders.find({"customer": {"$in": ["x"]}})`,
	} {
		run(h, bearer, statement(s), 403, "query_result_disclosure_blocked")
	}
	run(h, bearer, model.QueryExecuteRequest{Statement: `db.orders.find({"status": "open"})`, Database: "other"}, 400, "validation_failed")
	run(h, bearer, model.QueryExecuteRequest{Statement: `db.orders.find({"status": "open"})`, Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 5}}, 400, "validation_failed")

	// Approval maxTimeMS bounds the server-side scan: an index-backed plan that
	// still must fetch ~100k documents expires server-side.
	manifest["maxTimeMS"] = 50
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, statement(`db.orders.find({"status": "open", "region": "nope"})`), 408, "query_timeout")
	deadline := time.Now().Add(time.Second)
	for {
		if len(mongoOpsOn(t, ctx, admin, "demo.orders")) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("MongoDB operation still running after query_timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	delete(manifest, "maxTimeMS")
	if err := publish(); err != nil {
		t.Fatal(err)
	}

	// Revocations at each layer refuse the next Run.
	if err := demo.RunCommand(ctx, bson.D{
		{Key: "revokeRolesFromUser", Value: "reader"},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "worksheetReader"}, {Key: "db", Value: "demo"}}}},
	}).Err(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 403, "query_not_allowed")
	if err := demo.RunCommand(ctx, bson.D{
		{Key: "grantRolesToUser", Value: "reader"},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "worksheetReader"}, {Key: "db", Value: "demo"}}}},
	}).Err(); err != nil {
		t.Fatal(err)
	}
	delete(collections["orders"], "status")
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 403, "query_result_disclosure_blocked")
	collections["orders"]["status"] = raw
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	manifest["users"] = []uint64{}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	assertTarget(h, bearer, false)
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 403, "query_not_allowed")
	manifest["users"] = []uint64{user}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	if restored := run(h, bearer, statement(`db.orders.find({"status": "open"})`), 200, ""); len(restored.Rows) == 0 {
		t.Fatal("restored approval did not run")
	}

	dropTrigger := func() error {
		_, err := db.Exec(`DROP TRIGGER IF EXISTS mongo_worksheet_audit_failure`)
		return err
	}
	breakAudit := func() error {
		_, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER mongo_worksheet_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW BEGIN IF NEW.target_resource_id = %d AND NEW.event_type = 'query.executed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fixture audit failure'; END IF; END`, target.ID))
		return err
	}
	t.Cleanup(func() {
		if err := dropTrigger(); err != nil {
			t.Error(err)
		}
	})
	countPair := func() int {
		t.Helper()
		var hCount, aCount int
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM query_executions WHERE target_resource_id=?),(SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed')`, target.ID, target.ID).Scan(&hCount, &aCount); err != nil || hCount != aCount {
			t.Fatalf("MongoDB evidence mismatch: history=%d audit=%d err=%v", hCount, aCount, err)
		}
		return hCount
	}
	before := countPair()
	if err := breakAudit(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 502, "query_backend_error")
	if countPair() != before {
		t.Fatal("audit refusal left partial MongoDB evidence")
	}
	if err := dropTrigger(); err != nil {
		t.Fatal(err)
	}

	// Per-(target,user) minute bucket: capacity 2, so the third Run is refused
	// before MongoDB is contacted.
	manifest["limits"] = map[string]any{"maxConcurrentRuns": 16, "maxConcurrentRunsPerUser": 4, "runsPerSecond": 50, "runsPerMinutePerUser": 2}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 200, "")
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 200, "")
	run(h, bearer, statement(`db.orders.find({"status": "open"})`), 429, "query_rate_limited")
	var rejected429 int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND status='rejected' AND error_code='query_rate_limited'`, target.ID).Scan(&rejected429); err != nil || rejected429 != 1 {
		t.Fatalf("rate-limited evidence rows: %d: %v", rejected429, err)
	}

	var privateEvidence int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND (statement_preview LIKE '%orders%' OR statement_preview LIKE '%status%' OR full_statement IS NOT NULL OR error_message LIKE '%cn-east%' OR statement_digest<>'')`, target.ID).Scan(&privateEvidence); err != nil || privateEvidence != 0 {
		t.Fatalf("MongoDB persisted protected evidence: %d: %v", privateEvidence, err)
	}
	var unlabeled int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND statement_preview NOT IN ('MongoDB find') AND NOT (statement_preview='' AND status='rejected' AND error_code='query_not_allowed')`, target.ID).Scan(&unlabeled); err != nil || unlabeled != 0 {
		t.Fatalf("MongoDB statement previews left the fixed label: %d: %v", unlabeled, err)
	}
}
