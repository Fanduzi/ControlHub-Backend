package service

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var mongoOIDText = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// mongoCell is one converted result field: value is nil for missing or
// suppressed fields; truncated marks an over-limit or invalid-UTF-8 field.
type mongoCell struct {
	value     any
	truncated bool
}

// mongoFindQuery is the parsed and approved form of
// db.<collection>.find(<filter>[, <projection>]).
type mongoFindQuery struct {
	collection string
	filter     bson.D
	columns    []string // output column order, approved fields only
}

func (q mongoFindQuery) overFlag(field string) string {
	return "__controlhub_over_" + field
}

// cappedProjection emits a server-side projection that bounds every approved
// column to mongoFieldValueLimit bytes before it can cross the wire: a field
// over the bound is removed and its __controlhub_over_<field> flag is true.
// _id is excluded whenever it is not one of the returned columns.
func (q mongoFindQuery) cappedProjection() bson.D {
	included := make(map[string]bool, len(q.columns))
	projection := bson.D{}
	for _, field := range q.columns {
		included[field] = true
		size := bson.D{{Key: "$bsonSize", Value: bson.D{{Key: "v", Value: "$" + field}}}}
		projection = append(projection,
			bson.E{Key: field, Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$lte", Value: bson.A{size, mongoFieldValueLimit}}}, "$" + field, "$$REMOVE"}}}},
			bson.E{Key: q.overFlag(field), Value: bson.D{{Key: "$gt", Value: bson.A{size, mongoFieldValueLimit}}}},
		)
	}
	if !included["_id"] {
		projection = append(projection, bson.E{Key: "_id", Value: 0})
	}
	return projection
}

// ---- strict JSON value model ---------------------------------------------

type mongoJSONKind int

const (
	mongoJSONNull mongoJSONKind = iota
	mongoJSONBool
	mongoJSONNumber
	mongoJSONString
	mongoJSONArray
	mongoJSONObject
)

type mongoJSONEntry struct {
	key   string
	value mongoJSONValue
}

type mongoJSONValue struct {
	kind mongoJSONKind
	b    bool
	num  json.Number
	s    string
	arr  []mongoJSONValue
	obj  []mongoJSONEntry
}

const mongoJSONMaxDepth = 8

// mongoReadJSON decodes one strict JSON value from the token stream: objects
// preserve entry order, duplicate keys are refused, and numbers stay exact.
func mongoReadJSON(d *json.Decoder, depth int) (mongoJSONValue, error) {
	if depth > mongoJSONMaxDepth {
		return mongoJSONValue{}, ErrQueryValidationFailed
	}
	tok, err := d.Token()
	if err != nil {
		return mongoJSONValue{}, ErrQueryValidationFailed
	}
	switch t := tok.(type) {
	case nil:
		return mongoJSONValue{kind: mongoJSONNull}, nil
	case bool:
		return mongoJSONValue{kind: mongoJSONBool, b: t}, nil
	case string:
		return mongoJSONValue{kind: mongoJSONString, s: t}, nil
	case json.Number:
		return mongoJSONValue{kind: mongoJSONNumber, num: t}, nil
	case json.Delim:
		switch t {
		case '[':
			var arr []mongoJSONValue
			for d.More() {
				item, err := mongoReadJSON(d, depth+1)
				if err != nil {
					return mongoJSONValue{}, err
				}
				arr = append(arr, item)
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return mongoJSONValue{}, ErrQueryValidationFailed
			}
			return mongoJSONValue{kind: mongoJSONArray, arr: arr}, nil
		case '{':
			var obj []mongoJSONEntry
			seen := map[string]bool{}
			for d.More() {
				keyTok, err := d.Token()
				if err != nil {
					return mongoJSONValue{}, ErrQueryValidationFailed
				}
				key, ok := keyTok.(string)
				if !ok || seen[key] {
					return mongoJSONValue{}, ErrQueryValidationFailed
				}
				seen[key] = true
				value, err := mongoReadJSON(d, depth+1)
				if err != nil {
					return mongoJSONValue{}, err
				}
				obj = append(obj, mongoJSONEntry{key: key, value: value})
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return mongoJSONValue{}, ErrQueryValidationFailed
			}
			return mongoJSONValue{kind: mongoJSONObject, obj: obj}, nil
		}
	}
	return mongoJSONValue{}, ErrQueryValidationFailed
}

// ---- scalar conversion ----------------------------------------------------

// mongoScalar turns one accepted JSON scalar (or EJSON wrapper object) into a
// BSON value for the filter document.
func mongoScalar(v mongoJSONValue) (any, error) {
	switch v.kind {
	case mongoJSONNull:
		return nil, nil
	case mongoJSONBool:
		return v.b, nil
	case mongoJSONString:
		return v.s, nil
	case mongoJSONNumber:
		if i, err := v.num.Int64(); err == nil {
			return i, nil
		}
		f, err := v.num.Float64()
		if err != nil {
			return nil, ErrQueryValidationFailed
		}
		return f, nil
	case mongoJSONObject:
		return mongoEJSON(v)
	}
	return nil, ErrQueryValidationFailed
}

// mongoEJSON accepts exactly the three supported Extended JSON wrappers as
// scalar values: {"$oid": "<24 hex>"}, {"$date": "<RFC3339>"} and
// {"$numberLong": "<int64>"}.
func mongoEJSON(v mongoJSONValue) (any, error) {
	if len(v.obj) != 1 || v.obj[0].value.kind != mongoJSONString {
		return nil, ErrQueryValidationFailed
	}
	s := v.obj[0].value.s
	switch v.obj[0].key {
	case "$oid":
		if !mongoOIDText.MatchString(s) {
			return nil, ErrQueryValidationFailed
		}
		oid, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, ErrQueryValidationFailed
		}
		return oid, nil
	case "$date":
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, ErrQueryValidationFailed
		}
		return bson.NewDateTimeFromTime(parsed), nil
	case "$numberLong":
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, ErrQueryValidationFailed
		}
		return i, nil
	}
	return nil, ErrQueryValidationFailed
}

var mongoFilterOperators = map[string]bool{"$eq": true, "$gt": true, "$gte": true, "$lt": true, "$lte": true, "$in": true}

// mongoFilterValue converts one filter field value: a scalar (equality) or an
// operator object whose keys all belong to the whitelisted comparison set.
func mongoFilterValue(v mongoJSONValue) (any, error) {
	if v.kind != mongoJSONObject {
		return mongoScalar(v)
	}
	if len(v.obj) == 0 {
		return nil, ErrQueryValidationFailed
	}
	// EJSON wrappers are single-key objects; check them before the operator
	// rule so {"$oid": ...} parses as a scalar.
	if value, err := mongoEJSON(v); err == nil {
		return value, nil
	}
	ops := bson.D{}
	for _, entry := range v.obj {
		if !mongoFilterOperators[entry.key] {
			return nil, ErrQueryValidationFailed
		}
		if entry.key == "$in" {
			if entry.value.kind != mongoJSONArray || len(entry.value.arr) < 1 || len(entry.value.arr) > 100 {
				return nil, ErrQueryValidationFailed
			}
			items := bson.A{}
			for _, item := range entry.value.arr {
				scalar, err := mongoScalar(item)
				if err != nil {
					return nil, err
				}
				items = append(items, scalar)
			}
			ops = append(ops, bson.E{Key: "$in", Value: items})
			continue
		}
		scalar, err := mongoScalar(entry.value)
		if err != nil {
			return nil, err
		}
		ops = append(ops, bson.E{Key: entry.key, Value: scalar})
	}
	return ops, nil
}

// ---- statement grammar ----------------------------------------------------

// parseMongoFind parses and validates `db.<collection>.find(<filter>[, <proj>])`
// against the approval. Field-shape failures are validation_failed; unapproved
// collections or fields are disclosure_blocked.
func parseMongoFind(statement string, a mongoApproval) (mongoFindQuery, error) {
	var query mongoFindQuery
	match := mongoFind.FindStringSubmatch(statement)
	if match == nil {
		return query, ErrQueryValidationFailed
	}
	fields, approved := a.Collections[match[1]]
	if !approved {
		return query, ErrQueryDisclosureBlocked
	}
	query.collection = match[1]
	// A nil bson.D marshals as null, not {}; the empty filter must stay a
	// document or the server rejects it with TypeMismatch.
	query.filter = bson.D{}

	// The argument separators are not JSON tokens, so the parse walks the
	// decoder's byte offset: one object, an optional comma + object, then a
	// closing paren and nothing else.
	args := statement[len(match[0]):]
	filterValue, rest, err := mongoReadObjectArg(args)
	if err != nil {
		return query, err
	}
	var projectionValue *mongoJSONValue
	if rest = strings.TrimLeft(rest, " \t\r\n"); strings.HasPrefix(rest, ",") {
		var projection mongoJSONValue
		if projection, rest, err = mongoReadObjectArg(rest[1:]); err != nil {
			return query, err
		}
		projectionValue = &projection
		rest = strings.TrimLeft(rest, " \t\r\n")
	}
	if rest != ")" {
		return query, ErrQueryValidationFailed
	}

	if len(filterValue.obj) > mongoFilterFieldMax {
		return query, ErrQueryValidationFailed
	}
	for _, entry := range filterValue.obj {
		if !mongoFieldNameValid(entry.key) {
			return query, ErrQueryValidationFailed
		}
		// Filtering on a masked field would let a user confirm guessed values
		// by equality, so filter fields require raw disclosure (the ClickHouse
		// predicate rule). Masked fields stay projection-only.
		if mode, ok := fields[entry.key]; !ok || mode != model.ResultDisclosureRawCopyAllowed {
			return query, ErrQueryDisclosureBlocked
		}
		value, err := mongoFilterValue(entry.value)
		if err != nil {
			return query, err
		}
		query.filter = append(query.filter, bson.E{Key: entry.key, Value: value})
	}

	columns, err := mongoColumns(projectionValue, fields)
	if err != nil {
		return query, err
	}
	query.columns = columns
	return query, nil
}

// mongoReadObjectArg decodes one strict JSON object at the head of s and
// returns the remaining bytes so the caller owns separator handling.
func mongoReadObjectArg(s string) (mongoJSONValue, string, error) {
	var none mongoJSONValue
	decoder := json.NewDecoder(strings.NewReader(s))
	decoder.UseNumber()
	value, err := mongoReadJSON(decoder, 0)
	if err != nil || value.kind != mongoJSONObject {
		return none, "", ErrQueryValidationFailed
	}
	return value, s[decoder.InputOffset():], nil
}

// mongoColumns resolves the ordered result columns: projection order when a
// projection was given, else _id (when approved) followed by the sorted
// approved fields. _id is a column only when approved.
func mongoColumns(projection *mongoJSONValue, fields map[string]model.ResultDisclosureMode) ([]string, error) {
	if projection == nil {
		columns := make([]string, 0, len(fields))
		for field := range fields {
			columns = append(columns, field)
		}
		slices.Sort(columns)
		if _, ok := fields["_id"]; ok {
			return append([]string{"_id"}, slices.DeleteFunc(slices.Clone(columns), func(f string) bool { return f == "_id" })...), nil
		}
		return columns, nil
	}
	if len(projection.obj) == 0 {
		return nil, ErrQueryValidationFailed
	}
	columns := make([]string, 0, len(projection.obj))
	for _, entry := range projection.obj {
		if !mongoFieldNameValid(entry.key) {
			return nil, ErrQueryValidationFailed
		}
		include := entry.value.kind == mongoJSONBool && entry.value.b ||
			entry.value.kind == mongoJSONNumber && entry.value.num.String() == "1"
		exclude := entry.value.kind == mongoJSONBool && !entry.value.b ||
			entry.value.kind == mongoJSONNumber && entry.value.num.String() == "0"
		// "_id": 0 never discloses anything, so it needs no approval entry.
		if entry.key == "_id" && exclude {
			continue
		}
		if _, ok := fields[entry.key]; !ok {
			return nil, ErrQueryDisclosureBlocked
		}
		if !include {
			return nil, ErrQueryValidationFailed
		}
		columns = append(columns, entry.key)
	}
	if len(columns) == 0 {
		return nil, ErrQueryValidationFailed
	}
	return columns, nil
}

// ---- index gate -----------------------------------------------------------

// mongoIndexStages are the plan stages that constitute an index access path.
var mongoIndexStages = map[string]bool{
	"IXSCAN": true, "IDHACK": true, "RECORDID_FAST_PATH": true,
	"EXPRESS_IXSCAN": true, "EXPRESS_CLUSTERED_IXSCAN": true,
}

// mongoPlanWrappers are stages allowed to sit on top of the index access.
var mongoPlanWrappers = map[string]bool{
	"FETCH": true, "LIMIT": true, "SHARDING_FILTER": true,
	"PROJECTION_SIMPLE": true, "PROJECTION_DEFAULT": true, "PROJECTION_COVERED": true,
}

// mongoIndexGate walks queryPlanner.winningPlan and accepts only trees made
// of known wrappers around exactly one index access stage. Any other stage —
// COLLSCAN, SUBPLAN, OR, SORT, or something newer we do not model — fails
// closed.
func mongoIndexGate(out bson.M) (indexName string, ok bool) {
	planner := mongoDocM(out["queryPlanner"])
	if planner == nil {
		return "", false
	}
	plan := mongoDocM(planner["winningPlan"])
	if plan == nil {
		return "", false
	}
	indexes := 0
	for plan != nil {
		if inner := mongoDocM(plan["queryPlan"]); inner != nil && plan["stage"] == nil {
			plan = inner
			continue
		}
		stage, _ := plan["stage"].(string)
		switch {
		case mongoIndexStages[stage]:
			indexes++
			if name, _ := plan["indexName"].(string); indexName == "" {
				indexName = name
			}
		case mongoPlanWrappers[stage]:
		default:
			return "", false
		}
		var next bson.M
		if stage == "" || mongoPlanWrappers[stage] {
			if input := mongoDocM(plan["inputStage"]); input != nil {
				next = input
			} else if children, _ := plan["inputStages"].(bson.A); len(children) == 1 {
				next = mongoDocM(children[0])
			}
		}
		plan = next
	}
	return indexName, indexes == 1 && indexName != ""
}

// mongoDocM normalizes a nested document value (bson.D or bson.M) to bson.M.
func mongoDocM(v any) bson.M {
	switch d := v.(type) {
	case bson.M:
		return d
	case map[string]any:
		return bson.M(d)
	case bson.D:
		m := make(bson.M, len(d))
		for _, e := range d {
			m[e.Key] = e.Value
		}
		return m
	}
	return nil
}

// ---- cell values ----------------------------------------------------------

// mongoCellValue converts a BSON field value into its result-cell form.
// The bool marks the cell truncated: invalid UTF-8 strings never surface.
func mongoCellValue(v any) (any, bool) {
	switch value := v.(type) {
	case nil:
		return nil, false
	case string:
		if !utf8.ValidString(value) {
			return nil, true
		}
		return value, false
	case bool:
		return value, false
	case int32:
		return int64(value), false
	case int64:
		if value > 1<<53-1 || value < -(1<<53-1) {
			return strconv.FormatInt(value, 10), false
		}
		return value, false
	case float64:
		return value, false
	case bson.ObjectID:
		return value.Hex(), false
	case bson.DateTime:
		return value.Time().UTC().Format(time.RFC3339Nano), false
	case bson.Decimal128:
		return value.String(), false
	default:
		raw, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, true, false)
		if err != nil {
			return nil, true
		}
		text := string(raw)
		if strings.HasPrefix(text, `{"v":`) && strings.HasSuffix(text, "}") {
			return text[len(`{"v":`) : len(text)-1], false
		}
		return nil, true
	}
}
