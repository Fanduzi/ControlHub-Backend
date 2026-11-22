package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoTestApproval() mongoApproval {
	raw := model.ResultDisclosureRawCopyAllowed
	return mongoApproval{
		Database: "demo",
		Collections: map[string]map[string]model.ResultDisclosureMode{
			"orders": {"_id": raw, "orderNo": raw, "status": raw, "region": raw, "customer": model.ResultDisclosureMaskedNoCopy},
		},
		maxTimeMS: 500,
	}
}

func TestMongoFindGrammarAccepts(t *testing.T) {
	a := mongoTestApproval()
	for _, statement := range []string{
		`db.orders.find({})`,
		`db.orders.find({"status": "open"})`,
		`db.orders.find({"status":"open"},{"orderNo":1,"status":1})`,
		`db.orders.find({"status":"open"},{"status":true,"_id":0})`,
		`db.orders.find({"orderNo":{"$gte":100,"$lt":105}})`,
		`db.orders.find({"status":{"$in":["open","shipped"]}})`,
		`db.orders.find({"orderNo":{"$eq":1.5}})`,
		`db.orders.find({"status":{"$eq":"open"},"orderNo":{"$gte":0}})`,
		`db.orders.find({"status":null})`,
		`db.orders.find({"status":true})`,
		`db.orders.find({"_id":{"$oid":"0123456789abcdef01234567"}})`,
		`db.orders.find({"orderNo":{"$numberLong":"9007199254740993"}})`,
		`db.orders.find({"region":{"$date":"2026-01-01T00:00:00Z"}})`,
		`db.orders.find({"status":{"$in":[{"$numberLong":"1"}]}})`,
		`db.orders.find( { "status" : "open" } , { "status" : 1 } )`,
		`db.orders.find({"status":"open"}, {"orderNo":1,"status":1,"_id":0})`,
		`db.orders.find({"status":"open"}).sort({"status":1})`,
		`db.orders.find({"orderNo":{"$gte":100,"$lt":110}}).sort({"orderNo":-1})`,
		`db.orders.find({"status":"open"},{"orderNo":1}).sort({"status":-1,"orderNo":1})`,
		`db.orders.find({}).sort({"_id":-1})`,
	} {
		query, err := parseMongoFind(statement, a)
		if err != nil {
			t.Fatalf("accepted statement rejected: %s: %v", statement, err)
		}
		if query.collection != "orders" {
			t.Fatalf("collection lost: %s", statement)
		}
	}
}

func TestMongoFindGrammarRejects(t *testing.T) {
	a := mongoTestApproval()
	for _, statement := range []string{
		`db.orders.find({"$where":"sleep(100)||true"})`,
		`db.orders.find({"status":{"$function":"f()"}})`,
		`db.orders.find({"$expr":{"$eq":["$a",1]}})`,
		`db.orders.find({"status":{"$regex":"^o"}})`,
		`db.orders.find({"$or":[{"status":"open"}]})`,
		`db.orders.find({"$and":[{"status":"open"}]})`,
		`db.orders.find({"$nor":[{"status":"open"}]})`,
		`db.orders.find({"status":{"$not":{"$eq":"open"}}})`,
		`db.orders.find({"status":{"$ne":"open"}})`,
		`db.orders.find({"status":{"$nin":["open"]}})`,
		`db.orders.find({"status":{"$exists":true}})`,
		`db.orders.find({"tags":{"$elemMatch":{"$eq":"x"}}})`,
		`db.orders.find({"customer.name":"x"})`,
		`db.orders.find({"status":{"$eq":{"nested":"doc"}}})`,
		`db.orders.find({"status":{"$eq":"open","extra":1}})`,
		`db.orders.find({"status":{"$in":[]}})`,
		`db.orders.find({"status":{"$in":["open",{"k":"v"}]}})`,
		`db.orders.find({"status":["open","shipped"]})`,
		`db.orders.find({"status":"open","status":"open"})`,
		`db.orders.find({status:"open"})`,
		`db.orders.find({"status":"open"} extra)`,
		`db.orders.find({"status":"open"}).limit(1)`,
		`db.orders.sort({"orderNo":1}).find({})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":0})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":2})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":"asc"})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":true})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1.0})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1.5})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":[1]})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":{"$meta":"textScore"}})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1,"status":1,"region":1,"note":1,"_id":1})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1,"orderNo":-1})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1}).sort({"status":1})`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1}).limit(1)`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1}) trailing`,
		`db.orders.find({"status":"open"}).sort({"orderNo":1},{})`,
		`db.orders.find({"status":"open"}).sort ({"orderNo":1})`,
		`db.orders.find({"status":"open"}).sort()`,
		`db.orders.find({"status":"open"}).sort({"customer.name":1})`,
		`db.orders.find({"status":"open"}).sort({"$where":1})`,
		`db.orders.find({"status":"open"},{"orderNo":1}).sort({})`,
		`db.orders.aggregate([{"$match":{"status":"open"}}])`,
		`db.orders.findOne({"status":"open"})`,
		`db.orders.find()`,
		`db.orders.find({"status":"open"},)`,
		`db.orders.find({"status":"open"},{"orderNo"})`,
		`db.orders.find({"status":"open"},{"orderNo":0})`,
		`db.orders.find({"status":"open"},{"orderNo":"1"})`,
		`db.orders.find({"status":"open"},{"orderNo":{"$slice":1}})`,
		`db.orders.find({"status":"open"},{"orderNo":1},{"extra":1})`,
		`db.orders.find({"status":"open"},{"orderNo":1} , {"status":1})`,
		`db.orders.find({"status":{"$oid":"not-hex"}})`,
		`db.orders.find({"status":{"$oid":"0123456789abcdef0123456"}})`,
		`db.orders.find({"status":{"$date":"not-a-date"}})`,
		`db.orders.find({"status":{"$numberLong":"12.5"}})`,
		`db.orders.find({"status":{"$oid":"0123456789abcdef01234567","x":1}})`,
		`db.other.find({"status":"open"})`,
		`db..find({})`,
		`orders.find({"status":"open"})`,
		` db.orders.find({"status":"open"}) x`,
		`db.orders.find({"customer":{"name":"x"}})`,
	} {
		if _, err := parseMongoFind(statement, a); err == nil {
			t.Fatalf("rejected statement accepted: %s", statement)
		}
	}
}

func TestMongoFindGrammarRejectsTooManyFields(t *testing.T) {
	// Distinct keys required: build 17 approved-field keys via a wider approval.
	wide := mongoTestApproval()
	for i := 0; i < 20; i++ {
		wide.Collections["orders"]["k"+string(rune('a'+i))] = model.ResultDisclosureRawCopyAllowed
	}
	filter := "{"
	for i := 0; i <= mongoFilterFieldMax; i++ {
		if i > 0 {
			filter += ","
		}
		filter += `"k` + string(rune('a'+i)) + `":1`
	}
	filter += "}"
	if _, err := parseMongoFind("db.orders.find("+filter+")", wide); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("17-field filter accepted: %v", err)
	}
}

func TestMongoFindDisclosure(t *testing.T) {
	a := mongoTestApproval()
	for _, tc := range []struct {
		statement string
		want      error
	}{
		{`db.secrets.find({})`, ErrQueryDisclosureBlocked},
		{`db.orders.find({"secret":"x"})`, ErrQueryDisclosureBlocked},
		{`db.orders.find({"status":"open"},{"secret":1})`, ErrQueryDisclosureBlocked},
		{`db.orders.find({"status":"open"},{"_id":1,"secret":1})`, ErrQueryDisclosureBlocked},
		// customer is approved masked_no_copy: filterable would leak the value
		// through equality, so the filter itself is disclosure-blocked.
		{`db.orders.find({"customer":"x"})`, ErrQueryDisclosureBlocked},
		{`db.orders.find({"customer":{"$in":["x","y"]}})`, ErrQueryDisclosureBlocked},
		// Sorting on an unapproved or masked field leaks its order.
		{`db.orders.find({"status":"open"}).sort({"secret":1})`, ErrQueryDisclosureBlocked},
		{`db.orders.find({"status":"open"}).sort({"customer":1})`, ErrQueryDisclosureBlocked},
	} {
		if _, err := parseMongoFind(tc.statement, a); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.statement, err, tc.want)
		}
	}
}

func TestMongoFindColumns(t *testing.T) {
	a := mongoTestApproval()
	query, err := parseMongoFind(`db.orders.find({"status":"open"},{"region":1,"orderNo":1})`, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.columns) != 2 || query.columns[0] != "region" || query.columns[1] != "orderNo" {
		t.Fatalf("projection order lost: %v", query.columns)
	}
	query, err = parseMongoFind(`db.orders.find({"status":"open"})`, a)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"_id", "customer", "orderNo", "region", "status"}
	if len(query.columns) != len(want) {
		t.Fatalf("default columns: %v", query.columns)
	}
	for i := range want {
		if query.columns[i] != want[i] {
			t.Fatalf("default columns: %v", query.columns)
		}
	}
	// _id dropped from columns when excluded.
	query, err = parseMongoFind(`db.orders.find({"status":"open"},{"orderNo":1,"_id":0})`, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.columns) != 1 || query.columns[0] != "orderNo" {
		t.Fatalf("_id exclusion: %v", query.columns)
	}
	// Masked fields stay projectable — masking happens on the result, while
	// the same field is refused as a filter key (TestMongoFindDisclosure).
	query, err = parseMongoFind(`db.orders.find({"status":"open"},{"customer":1})`, a)
	if err != nil || len(query.columns) != 1 || query.columns[0] != "customer" {
		t.Fatalf("masked projection refused: %v %v", query.columns, err)
	}
	// Approval without _id: default columns carry no _id.
	withoutID := mongoTestApproval()
	delete(withoutID.Collections["orders"], "_id")
	query, err = parseMongoFind(`db.orders.find({"status":"open"})`, withoutID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range query.columns {
		if c == "_id" {
			t.Fatalf("unapproved _id became a column: %v", query.columns)
		}
	}
	if _, err := parseMongoFind(`db.orders.find({"_id":{"$oid":"0123456789abcdef01234567"}})`, withoutID); !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("unapproved _id filter: %v", err)
	}
}

func TestMongoCappedProjection(t *testing.T) {
	a := mongoTestApproval()
	query, err := parseMongoFind(`db.orders.find({"status":"open"},{"orderNo":1})`, a)
	if err != nil {
		t.Fatal(err)
	}
	projection := query.cappedProjection()
	var idSeen, fieldSeen, flagSeen bool
	for _, e := range projection {
		switch e.Key {
		case "_id":
			idSeen = true
			if e.Value != 0 {
				t.Fatalf("_id must be excluded: %v", e.Value)
			}
		case "orderNo":
			fieldSeen = true
		case query.overFlag("orderNo"):
			flagSeen = true
		}
	}
	if !idSeen || !fieldSeen || !flagSeen || len(projection) != 3 {
		t.Fatalf("capped projection shape: %v", projection)
	}
	raw, err := json.Marshal(projection)
	if err != nil || len(raw) == 0 {
		t.Fatalf("projection did not marshal: %v", err)
	}
}

func TestMongoFindSort(t *testing.T) {
	a := mongoTestApproval()
	query, err := parseMongoFind(`db.orders.find({"status":"open"}).sort({"status":-1,"orderNo":1})`, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.sort) != 2 || query.sort[0].Key != "status" || query.sort[0].Value != int64(-1) ||
		query.sort[1].Key != "orderNo" || query.sort[1].Value != int64(1) {
		t.Fatalf("sort document: %+v", query.sort)
	}
}

// Plan fixtures captured live from mongo:8.0.38 and mongo:9.0.8
// (explain verbosity queryPlanner, find limit 51).
var mongoPlanFixtures = []struct {
	version, name, json string
	ok                  bool
	index               string
}{
	{"8.0.38", "status_eq", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"status":1},"indexName":"status_1","isMultiKey":false,"multiKeyPaths":{"status":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]"]}}}}`, true, "status_1"},
	{"8.0.38", "region_eq", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"COLLSCAN","filter":{"region":{"$eq":"cn-east"}},"direction":"forward"}}`, false, ""},
	{"8.0.38", "empty", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"COLLSCAN","direction":"forward"}}`, false, ""},
	{"8.0.38", "orderNo_range", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"orderNo":1},"indexName":"orderNo_1","isMultiKey":false,"multiKeyPaths":{"orderNo":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"orderNo":["[500000, 500050)"]}}}}`, true, "orderNo_1"},
	{"8.0.38", "status_region_residual", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","filter":{"region":{"$eq":"x"}},"inputStage":{"stage":"IXSCAN","keyPattern":{"status":1},"indexName":"status_1","isMultiKey":false,"multiKeyPaths":{"status":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]"]}}}}`, true, "status_1"},
	{"8.0.38", "status_in", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"status":1},"indexName":"status_1","isMultiKey":false,"multiKeyPaths":{"status":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]","[\"shipped\", \"shipped\"]"]}}}}`, true, "status_1"},
	{"8.0.38", "or_mixed", `{"isCached":false,"stage":"SUBPLAN","inputStage":{"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"COLLSCAN","filter":{"$or":[{"region":{"$eq":"x"}},{"status":{"$eq":"open"}}]},"direction":"forward"}}}`, false, ""},
	{"8.0.38", "id_point", `{"isCached":false,"stage":"EXPRESS_IXSCAN","keyPattern":"{ _id: 1 }","indexName":"_id_"}`, true, "_id_"},
	{"9.0.8", "status_eq", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","nss":"test.orders","inputStage":{"stage":"IXSCAN","nss":"test.orders","keyPattern":{"status":1},"indexName":"status_1","isMultiKey":false,"multiKeyPaths":{"status":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]"]}}}}`, true, "status_1"},
	{"9.0.8", "region_eq", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"COLLSCAN","filter":{"region":{"$eq":"cn-east"}},"nss":"test.orders","direction":"forward"}}`, false, ""},
	{"9.0.8", "id_point", `{"isCached":false,"stage":"EXPRESS_IXSCAN","keyPattern":{"_id":1},"indexName":"_id_"}`, true, "_id_"},
	{"9.0.8", "or_mixed", `{"isCached":false,"stage":"SUBPLAN","inputStage":{"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"COLLSCAN","filter":{"$or":[{"region":{"$eq":"x"}},{"status":{"$eq":"open"}}]},"direction":"forward"}}}`, false, ""},
	{"9.0.8", "sort_region", `{"isCached":false,"stage":"SORT","sortPattern":{"region":1},"memLimit":104857600,"limitAmount":51,"type":"simple","inputStage":{"stage":"FETCH","nss":"test.orders","inputStage":{"stage":"IXSCAN","nss":"test.orders","keyPattern":{"status":1},"indexName":"status_1","direction":"forward"}}}`, false, ""},
	{"8.0.38", "sort_orderNo_backward", `{"isCached":false,"stage":"LIMIT","limitAmount":11,"inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"orderNo":1},"indexName":"orderNo_1","isMultiKey":false,"multiKeyPaths":{"orderNo":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"backward","indexBounds":{"orderNo":["(10, 1]"]}}}}`, true, "orderNo_1"},
	{"8.0.38", "sort_compound", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"status":1,"amount":-1},"indexName":"status_1_amount_-1","isMultiKey":false,"multiKeyPaths":{"status":[],"amount":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]"],"amount":["[MaxKey, MinKey]"]}}}}`, true, "status_1_amount_-1"},
	{"8.0.38", "sort_region", `{"isCached":false,"stage":"SORT","sortPattern":{"region":1},"memLimit":104857600,"limitAmount":51,"type":"simple","inputStage":{"stage":"FETCH","inputStage":{"stage":"IXSCAN","keyPattern":{"status":1,"amount":-1},"indexName":"status_1_amount_-1","direction":"forward"}}}`, false, ""},
	{"9.0.8", "sort_orderNo_backward", `{"isCached":false,"stage":"LIMIT","limitAmount":11,"inputStage":{"stage":"FETCH","nss":"test.orders","inputStage":{"stage":"IXSCAN","nss":"test.orders","keyPattern":{"orderNo":1},"indexName":"orderNo_1","isMultiKey":false,"multiKeyPaths":{"orderNo":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"backward","indexBounds":{"orderNo":["(10, 1]"]}}}}`, true, "orderNo_1"},
	{"9.0.8", "sort_compound", `{"isCached":false,"stage":"LIMIT","limitAmount":51,"inputStage":{"stage":"FETCH","nss":"test.orders","inputStage":{"stage":"IXSCAN","nss":"test.orders","keyPattern":{"status":1,"amount":-1},"indexName":"status_1_amount_-1","isMultiKey":false,"multiKeyPaths":{"status":[],"amount":[]},"isUnique":false,"isSparse":false,"isPartial":false,"indexVersion":2,"direction":"forward","indexBounds":{"status":["[\"open\", \"open\"]"],"amount":["[MaxKey, MinKey]"]}}}}`, true, "status_1_amount_-1"},
}

func TestMongoIndexGate(t *testing.T) {
	for _, tc := range mongoPlanFixtures {
		var plan bson.M
		if err := json.Unmarshal([]byte(tc.json), &plan); err != nil {
			t.Fatalf("%s/%s fixture: %v", tc.version, tc.name, err)
		}
		out := bson.M{"queryPlanner": bson.M{"winningPlan": plan}}
		index, ok := mongoIndexGate(out)
		if ok != tc.ok || index != tc.index {
			t.Fatalf("%s/%s: got index=%q ok=%v, want %q %v", tc.version, tc.name, index, ok, tc.index, tc.ok)
		}
	}
	// Structural negatives.
	if _, ok := mongoIndexGate(bson.M{}); ok {
		t.Fatal("empty explain accepted")
	}
	if _, ok := mongoIndexGate(bson.M{"queryPlanner": bson.M{"winningPlan": bson.M{"stage": "AND_HASH", "inputStages": bson.A{
		bson.M{"stage": "IXSCAN", "indexName": "a"}, bson.M{"stage": "IXSCAN", "indexName": "b"},
	}}}}); ok {
		t.Fatal("multi-index plan accepted")
	}
	if _, ok := mongoIndexGate(bson.M{"queryPlanner": bson.M{"winningPlan": bson.M{"stage": "FETCH", "inputStage": bson.M{"stage": "SOMETHING_NEW"}}}}); ok {
		t.Fatal("unknown stage accepted")
	}
}

func TestMongoCellValue(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("0123456789abcdef01234567")
	for _, tc := range []struct {
		name      string
		in        any
		want      any
		truncated bool
	}{
		{"nil", nil, nil, false},
		{"string", "open", "open", false},
		{"invalid utf8", "\xff\xfe", nil, true},
		{"bool", true, true, false},
		{"int32", int32(5), int64(5), false},
		{"int64", int64(42), int64(42), false},
		{"int64 overflow", int64(1) << 60, "1152921504606846976", false},
		{"double", 1.5, 1.5, false},
		{"oid", oid, "0123456789abcdef01234567", false},
		{"datetime", bson.NewDateTimeFromTime(testTime), testTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), false},
		{"decimal", mustDecimal(t, "12.75"), "12.75", false},
		{"subdoc", bson.D{{Key: "name", Value: "n"}}, `{"name":"n"}`, false},
		{"array", bson.A{"a", "b"}, `["a","b"]`, false},
	} {
		got, truncated := mongoCellValue(tc.in)
		if truncated != tc.truncated || got != tc.want {
			t.Fatalf("%s: got (%v,%v) want (%v,%v)", tc.name, got, truncated, tc.want, tc.truncated)
		}
	}
}

var testTime = time.Date(2026, 2, 3, 4, 5, 6, 7000000, time.UTC)

func mustDecimal(t *testing.T, s string) bson.Decimal128 {
	t.Helper()
	d, err := bson.ParseDecimal128(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMongoApprovalValidation(t *testing.T) {
	for name, want := range map[string]bool{
		"_id": true, "status": true, "a1_": true, "orderNo": true,
		"customer.name": false, "__controlhub_over_x": false, "$set": false, "": false,
		"1abc": false, "_private": false,
	} {
		if got := mongoFieldNameValid(name); got != want {
			t.Fatalf("mongoFieldNameValid(%q)=%v want %v", name, got, want)
		}
	}
}

func TestMongoCredentialURL(t *testing.T) {
	base := "mongodb://reader:pw@127.0.0.1:27017/demo"
	cred, err := mongoCredentialURL(base, "127.0.0.1:27017", "demo")
	if err != nil || cred.username != "reader" || cred.password != "pw" || cred.authSource != "demo" {
		t.Fatalf("valid credential rejected: %v %+v", err, cred)
	}
	cred, err = mongoCredentialURL(base+"?authSource=admin", "127.0.0.1:27017", "demo")
	if err != nil || cred.authSource != "admin" {
		t.Fatalf("authSource param rejected: %v %+v", err, cred)
	}
	for _, tc := range []struct {
		dsn, address, db string
	}{
		{"mongodb+srv://reader:pw@127.0.0.1:27017/demo", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27018/demo", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/other", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/", "127.0.0.1:27017", "demo"},
		{"mongodb://reader@127.0.0.1:27017/demo", "127.0.0.1:27017", "demo"},
		{"mongodb://:pw@127.0.0.1:27017/demo", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/demo?replicaSet=rs0", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/demo?authSource=admin&x=1", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/demo?authSource=", "127.0.0.1:27017", "demo"},
		{"mongodb://reader:pw@127.0.0.1:27017/demo#frag", "127.0.0.1:27017", "demo"},
	} {
		if _, err := mongoCredentialURL(tc.dsn, tc.address, tc.db); !errors.Is(err, ErrQueryNotAllowed) {
			t.Fatalf("%s accepted: %v", tc.dsn, err)
		}
	}
	// Percent-encoded password survives.
	cred, err = mongoCredentialURL("mongodb://reader:p%40ss%3Ax@127.0.0.1:27017/demo", "127.0.0.1:27017", "demo")
	if err != nil || cred.password != "p@ss:x" {
		t.Fatalf("encoded password: %v %+v", err, cred)
	}
}
