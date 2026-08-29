// Package service provides controlled CI ingestion parsing and pure preview reconciliation.
// input: stdlib CSV/JSON/SHA-256 utilities and internal/model identity/relation types
// output: ParseIngestion, PreviewIngestion, bounded input and explicit preview diff contracts
// pos: Side-effect-free issue #83 ingestion checkpoint before persistence or API wiring
// note: if this file changes, update this header and module README.md.
package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

const (
	MaxIngestionRows     = 500
	MaxIngestionRowBytes = 64 << 10
	MaxIngestionBytes    = MaxIngestionRows * MaxIngestionRowBytes
)

type ObservedValueInput struct {
	Source string `json:"source"`
	Value  any    `json:"value"`
}

type IngestionRelation struct {
	Type     model.RelationType `json:"type"`
	TargetID uint64             `json:"targetId"`
}

type IngestionRow struct {
	EnvironmentID       uint64                             `json:"environmentId"`
	CIType              model.ResourceType                 `json:"ciType"`
	Name                string                             `json:"name"`
	DisplayName         string                             `json:"displayName,omitempty"`
	Aliases             []string                           `json:"aliases,omitempty"`
	ExternalIdentifiers []model.ResourceExternalIdentifier `json:"externalIdentifiers,omitempty"`
	Profile             map[string]any                     `json:"profile,omitempty"`
	ObservedValues      map[string]ObservedValueInput      `json:"observedValues,omitempty"`
	Relations           []IngestionRelation                `json:"relations,omitempty"`
}

type IngestionSnapshot struct {
	ID                  uint64
	EnvironmentID       uint64
	CIType              model.ResourceType
	Name                string
	DisplayName         string
	Aliases             []string
	ExternalIdentifiers []model.ResourceExternalIdentifier
	Profile             map[string]any
	ObservedValues      map[string]ObservedValueInput
	Relations           []IngestionRelation
	ManualOverrides     map[string]any `json:"-"`
}

type PreviewAction string

const (
	PreviewCreate   PreviewAction = "create"
	PreviewUpdate   PreviewAction = "update"
	PreviewConflict PreviewAction = "conflict"
)

type ValueDiff struct {
	Before any `json:"before"`
	After  any `json:"after"`
}
type RelationDiff struct {
	Added   []IngestionRelation `json:"added"`
	Removed []IngestionRelation `json:"removed"`
}
type IngestionDiff struct {
	Fields    map[string]ValueDiff `json:"fields"`
	Profile   map[string]ValueDiff `json:"profile"`
	Observed  map[string]ValueDiff `json:"observed"`
	Relations RelationDiff         `json:"relations"`
}
type IngestionPreviewRow struct {
	Row       int           `json:"row"`
	Action    PreviewAction `json:"action"`
	MatchedID uint64        `json:"matchedId,omitempty"`
	Conflict  string        `json:"conflict,omitempty"`
	Diff      IngestionDiff `json:"diff"`
}
type IngestionPreview struct {
	Confirmable bool                  `json:"confirmable"`
	Fingerprint string                `json:"fingerprint"`
	Rows        []IngestionPreviewRow `json:"rows"`
}

func ParseIngestion(format string, payload []byte) ([]IngestionRow, error) {
	if len(payload) == 0 || len(payload) > MaxIngestionBytes {
		return nil, errors.New("ingestion payload size is invalid")
	}
	var rows []IngestionRow
	var err error
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		rows, err = parseIngestionJSON(payload)
	case "csv":
		rows, err = parseIngestionCSV(payload)
	default:
		return nil, errors.New("ingestion format must be csv or json")
	}
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > MaxIngestionRows {
		return nil, fmt.Errorf("ingestion row count must be between 1 and %d", MaxIngestionRows)
	}
	seen := map[string]int{}
	for i := range rows {
		canonicalizeRow(&rows[i])
		if err := validateIngestionRow(rows[i]); err != nil {
			return nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		b, _ := json.Marshal(rows[i])
		if len(b) > MaxIngestionRowBytes {
			return nil, fmt.Errorf("row %d exceeds %d bytes", i+1, MaxIngestionRowBytes)
		}
		for _, key := range rowIdentityKeys(rows[i]) {
			if prior := seen[key]; prior != 0 {
				return nil, fmt.Errorf("row %d duplicates input identity from row %d", i+1, prior)
			}
			seen[key] = i + 1
		}
	}
	return rows, nil
}

func parseIngestionJSON(payload []byte) ([]IngestionRow, error) {
	var rawRows []json.RawMessage
	d := json.NewDecoder(bytes.NewReader(payload))
	if err := d.Decode(&rawRows); err != nil {
		return nil, fmt.Errorf("invalid ingestion JSON: %w", err)
	}
	if err := requireJSONEOF(d); err != nil {
		return nil, err
	}
	if len(rawRows) > MaxIngestionRows {
		return nil, fmt.Errorf("ingestion exceeds %d rows", MaxIngestionRows)
	}
	rows := make([]IngestionRow, len(rawRows))
	for i, raw := range rawRows {
		if len(raw) > MaxIngestionRowBytes {
			return nil, fmt.Errorf("row %d exceeds %d bytes", i+1, MaxIngestionRowBytes)
		}
		if err := decodeStrictJSON(raw, &rows[i]); err != nil {
			return nil, fmt.Errorf("invalid ingestion JSON row %d: %w", i+1, err)
		}
	}
	return rows, nil
}

func parseIngestionCSV(payload []byte) ([]IngestionRow, error) {
	r := csv.NewReader(bytes.NewReader(payload))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("invalid ingestion CSV: %w", err)
	}
	allowed := map[string]bool{"environmentId": true, "ciType": true, "name": true, "displayName": true, "aliases": true, "externalIdentifiers": true, "profile": true, "observedValues": true, "relations": true}
	indexes := map[string]int{}
	for i, h := range header {
		if !allowed[h] || indexes[h] != 0 {
			return nil, fmt.Errorf("unknown or duplicate CSV field %q", h)
		}
		indexes[h] = i + 1
	}
	for _, required := range []string{"environmentId", "ciType", "name"} {
		if indexes[required] == 0 {
			return nil, fmt.Errorf("missing CSV field %q", required)
		}
	}
	var rows []IngestionRow
	for {
		record, readErr := r.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("invalid ingestion CSV: %w", readErr)
		}
		if len(record) != len(header) {
			return nil, errors.New("CSV row has wrong field count")
		}
		rowBytes := 0
		for _, field := range record {
			rowBytes += len(field)
		}
		if rowBytes > MaxIngestionRowBytes {
			return nil, fmt.Errorf("CSV row exceeds %d bytes", MaxIngestionRowBytes)
		}
		if len(rows) >= MaxIngestionRows {
			return nil, fmt.Errorf("ingestion exceeds %d rows", MaxIngestionRows)
		}
		get := func(name string) string {
			if indexes[name] == 0 {
				return ""
			}
			return record[indexes[name]-1]
		}
		env, parseErr := strconv.ParseUint(strings.TrimSpace(get("environmentId")), 10, 64)
		if parseErr != nil {
			return nil, errors.New("invalid environmentId")
		}
		row := IngestionRow{EnvironmentID: env, CIType: model.ResourceType(get("ciType")), Name: get("name"), DisplayName: get("displayName")}
		for name, dst := range map[string]any{"aliases": &row.Aliases, "externalIdentifiers": &row.ExternalIdentifiers, "profile": &row.Profile, "observedValues": &row.ObservedValues, "relations": &row.Relations} {
			if cell := strings.TrimSpace(get(name)); cell != "" {
				if err := decodeStrictJSON([]byte(cell), dst); err != nil {
					return nil, fmt.Errorf("invalid CSV %s: %w", name, err)
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeStrictJSON(b []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return err
	}
	return requireJSONEOF(d)
}
func requireJSONEOF(d *json.Decoder) error {
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func canonicalizeRow(row *IngestionRow) {
	row.Name, row.DisplayName = strings.TrimSpace(row.Name), strings.TrimSpace(row.DisplayName)
	row.Aliases = canonicalAliases(row.Aliases)
	row.ExternalIdentifiers = canonicalExternalIdentifiers(row.ExternalIdentifiers)
	row.Relations = canonicalRelations(row.Relations)
	for k, v := range row.ObservedValues {
		v.Source = strings.TrimSpace(v.Source)
		row.ObservedValues[k] = v
	}
}

func validateIngestionRow(row IngestionRow) error {
	if row.EnvironmentID == 0 || row.Name == "" {
		return errors.New("environmentId and name are required")
	}
	if err := row.CIType.Validate(); err != nil {
		return err
	}
	for _, x := range row.ExternalIdentifiers {
		if x.System == "" || x.Value == "" {
			return errors.New("external identifier system and value are required")
		}
	}
	for _, x := range row.Relations {
		if x.TargetID == 0 {
			return errors.New("relation targetId is required")
		}
		if err := x.Type.Validate(); err != nil {
			return err
		}
	}
	for _, x := range row.ObservedValues {
		if x.Source == "" {
			return errors.New("observed value source is required")
		}
	}
	if containsSecret(row.Profile) || containsSecret(row.ObservedValues) {
		return errors.New("secret-bearing fields are not allowed")
	}
	return nil
}

func containsSecret(v any) bool {
	b, _ := json.Marshal(v)
	var generic any
	_ = json.Unmarshal(b, &generic)
	var walk func(any) bool
	walk = func(value any) bool {
		switch x := value.(type) {
		case map[string]any:
			for k, child := range x {
				key := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(k))
				if key == "password" || key == "secret" || key == "token" || key == "credential" || key == "dsn" || key == "apikey" || key == "privatekey" {
					return true
				}
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(generic)
}

func PreviewIngestion(rows []IngestionRow, current []IngestionSnapshot) IngestionPreview {
	p := IngestionPreview{Confirmable: true, Rows: make([]IngestionPreviewRow, len(rows))}
	fingerprintSnapshots := map[uint64]IngestionSnapshot{}
	for i, row := range rows {
		external, alias, name := matchSnapshots(row, current)
		all := unionIDs(external, alias, name)
		for id := range all {
			for _, snapshot := range current {
				if snapshot.ID == id {
					fingerprintSnapshots[id] = snapshot
					break
				}
			}
		}
		result := IngestionPreviewRow{Row: i + 1, Diff: emptyIngestionDiff()}
		chosen, conflict := chooseMatch(external, alias, name)
		if conflict != "" {
			result.Action, result.Conflict, p.Confirmable = PreviewConflict, conflict, false
		} else if chosen == 0 {
			result.Action, result.Diff = PreviewCreate, diffIngestion(row, IngestionSnapshot{})
		} else {
			result.Action, result.MatchedID = PreviewUpdate, chosen
			for _, snapshot := range current {
				if snapshot.ID == chosen {
					result.Diff = diffIngestion(row, snapshot)
					break
				}
			}
		}
		p.Rows[i] = result
	}
	p.Fingerprint = ingestionFingerprint(rows, fingerprintSnapshots)
	return p
}

func matchSnapshots(row IngestionRow, current []IngestionSnapshot) (map[uint64]bool, map[uint64]bool, map[uint64]bool) {
	external, alias, name := map[uint64]bool{}, map[uint64]bool{}, map[uint64]bool{}
	rowExternal, rowAliases := canonicalExternalIdentifiers(row.ExternalIdentifiers), canonicalAliases(row.Aliases)
	for _, s := range current {
		if intersectsExternal(rowExternal, canonicalExternalIdentifiers(s.ExternalIdentifiers)) {
			external[s.ID] = true
		}
		if row.EnvironmentID == s.EnvironmentID && intersectsStrings(rowAliases, canonicalAliases(s.Aliases)) {
			alias[s.ID] = true
		}
		if row.EnvironmentID == s.EnvironmentID && row.CIType == s.CIType && row.Name == s.Name {
			name[s.ID] = true
		}
	}
	return external, alias, name
}

func chooseMatch(stages ...map[uint64]bool) (uint64, string) {
	var chosen uint64
	for _, stage := range stages {
		if len(stage) > 1 {
			return 0, "identity matches multiple CIs"
		}
		for id := range stage {
			if chosen == 0 {
				chosen = id
			} else if chosen != id {
				return 0, "identity methods disagree"
			}
		}
	}
	return chosen, ""
}

func diffIngestion(row IngestionRow, before IngestionSnapshot) IngestionDiff {
	d := emptyIngestionDiff()
	addDiff(d.Fields, "environmentId", before.EnvironmentID, row.EnvironmentID)
	addDiff(d.Fields, "ciType", before.CIType, row.CIType)
	addDiff(d.Fields, "name", before.Name, row.Name)
	addDiff(d.Fields, "displayName", before.DisplayName, row.DisplayName)
	addDiff(d.Fields, "aliases", canonicalAliases(before.Aliases), canonicalAliases(row.Aliases))
	addDiff(d.Fields, "externalIdentifiers", canonicalExternalIdentifiers(before.ExternalIdentifiers), canonicalExternalIdentifiers(row.ExternalIdentifiers))
	diffMaps(d.Profile, before.Profile, row.Profile)
	oldObserved, newObserved := mapStringAny(before.ObservedValues), mapStringAny(row.ObservedValues)
	diffMaps(d.Observed, oldObserved, newObserved)
	oldRelations, newRelations := canonicalRelations(before.Relations), canonicalRelations(row.Relations)
	for _, relation := range newRelations {
		if !containsRelation(oldRelations, relation) {
			d.Relations.Added = append(d.Relations.Added, relation)
		}
	}
	for _, relation := range oldRelations {
		if !containsRelation(newRelations, relation) {
			d.Relations.Removed = append(d.Relations.Removed, relation)
		}
	}
	return d
}

func emptyIngestionDiff() IngestionDiff {
	return IngestionDiff{Fields: map[string]ValueDiff{}, Profile: map[string]ValueDiff{}, Observed: map[string]ValueDiff{}, Relations: RelationDiff{Added: []IngestionRelation{}, Removed: []IngestionRelation{}}}
}
func addDiff(dst map[string]ValueDiff, key string, before, after any) {
	if !reflect.DeepEqual(before, after) {
		dst[key] = ValueDiff{Before: before, After: after}
	}
}
func diffMaps(dst map[string]ValueDiff, before, after map[string]any) {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	for k := range keys {
		if !reflect.DeepEqual(before[k], after[k]) {
			dst[k] = ValueDiff{Before: before[k], After: after[k]}
		}
	}
}
func mapStringAny[T any](in map[string]T) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ingestionFingerprint(rows []IngestionRow, snapshots map[uint64]IngestionSnapshot) string {
	type cleanSnapshot struct {
		ID                  uint64                             `json:"id"`
		EnvironmentID       uint64                             `json:"environmentId"`
		CIType              model.ResourceType                 `json:"ciType"`
		Name                string                             `json:"name"`
		DisplayName         string                             `json:"displayName"`
		Aliases             []string                           `json:"aliases"`
		ExternalIdentifiers []model.ResourceExternalIdentifier `json:"externalIdentifiers"`
		Profile             map[string]any                     `json:"profile"`
		ObservedValues      map[string]ObservedValueInput      `json:"observedValues"`
		Relations           []IngestionRelation                `json:"relations"`
	}
	clean := make([]cleanSnapshot, 0, len(snapshots))
	for _, s := range snapshots {
		clean = append(clean, cleanSnapshot{s.ID, s.EnvironmentID, s.CIType, s.Name, s.DisplayName, canonicalAliases(s.Aliases), canonicalExternalIdentifiers(s.ExternalIdentifiers), s.Profile, s.ObservedValues, canonicalRelations(s.Relations)})
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i].ID < clean[j].ID })
	b, _ := json.Marshal(struct {
		Rows    []IngestionRow  `json:"rows"`
		Current []cleanSnapshot `json:"current"`
	}{rows, clean})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func rowIdentityKeys(row IngestionRow) []string {
	keys := []string{fmt.Sprintf("n:%d:%s:%s", row.EnvironmentID, row.CIType, row.Name)}
	for _, a := range row.Aliases {
		keys = append(keys, fmt.Sprintf("a:%d:%s", row.EnvironmentID, a))
	}
	for _, x := range row.ExternalIdentifiers {
		keys = append(keys, "x:"+x.System+":"+x.Value)
	}
	return keys
}
func canonicalAliases(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func canonicalExternalIdentifiers(in []model.ResourceExternalIdentifier) []model.ResourceExternalIdentifier {
	out := make([]model.ResourceExternalIdentifier, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v.System = strings.ToLower(strings.TrimSpace(v.System))
		v.Value = strings.TrimSpace(v.Value)
		key := v.System + "\x00" + v.Value
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].System < out[j].System || out[i].System == out[j].System && out[i].Value < out[j].Value
	})
	return out
}
func canonicalRelations(in []IngestionRelation) []IngestionRelation {
	out := append([]IngestionRelation(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Type < out[j].Type || out[i].Type == out[j].Type && out[i].TargetID < out[j].TargetID
	})
	dedup := out[:0]
	for _, v := range out {
		if len(dedup) == 0 || dedup[len(dedup)-1] != v {
			dedup = append(dedup, v)
		}
	}
	return dedup
}
func intersectsStrings(a, b []string) bool {
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if set[v] {
			return true
		}
	}
	return false
}
func intersectsExternal(a, b []model.ResourceExternalIdentifier) bool {
	set := map[string]bool{}
	for _, v := range a {
		set[v.System+"\x00"+v.Value] = true
	}
	for _, v := range b {
		if set[v.System+"\x00"+v.Value] {
			return true
		}
	}
	return false
}
func containsRelation(in []IngestionRelation, want IngestionRelation) bool {
	for _, v := range in {
		if v == want {
			return true
		}
	}
	return false
}
func unionIDs(sets ...map[uint64]bool) map[uint64]bool {
	out := map[uint64]bool{}
	for _, set := range sets {
		for id := range set {
			out[id] = true
		}
	}
	return out
}
