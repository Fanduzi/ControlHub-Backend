// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: github.com/pganalyze/pg_query_go/v6 protobuf AST, google.golang.org/protobuf protoreflect
// output: msgOf, walkCtx, walkVisit, walkTree — structural-path AST traversal shared by every transform
// pos: generic parse-tree walker; frames carry {parent message, field, list index, child} so visitors know WHERE a node sits (e.g. RangeVar via fromClause vs whereClause)
// note: if this file changes, update header and README.md
package pgsql

import (
	"reflect"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// msgOf converts a protobuf message (usually *pg.Node) to its reflective view,
// returning nil for typed-nil values so callers can nil-check once.
func msgOf(m protoreflect.ProtoMessage) protoreflect.Message {
	if m == nil {
		return nil
	}
	if v := reflect.ValueOf(m); v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}
	return m.ProtoReflect()
}

// frame is one step in the tree path.
type frame struct {
	parent string // message name that owns the field
	field  string // field inside parent that led to the child
	index  int    // list index for repeated fields, -1 for singular
	child  string // message name of the node this frame describes
}

// walkCtx is the per-node context handed to visitors.
type walkCtx struct {
	path []frame // root first; last element is the frame that reached m
}

// walkVisit is called once per message (including *pg.Node wrappers and the
// message they carry). Return false to skip a subtree.
type walkVisit func(ctx *walkCtx, m protoreflect.Message) bool

// walkTree descends m depth-first in field order.
func walkTree(m protoreflect.Message, visit walkVisit) {
	walkInto(&walkCtx{}, m, visit)
}

func walkInto(ctx *walkCtx, m protoreflect.Message, visit walkVisit) {
	if m == nil || !m.IsValid() {
		return
	}
	if !visit(ctx, m) {
		return
	}
	parentName := string(m.Descriptor().Name())
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsList() && fd.Kind() == protoreflect.MessageKind:
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				child := list.Get(i).Message()
				if !child.IsValid() {
					continue
				}
				ctx.path = append(ctx.path, frame{parent: parentName, field: string(fd.Name()), index: i, child: string(child.Descriptor().Name())})
				walkInto(ctx, child, visit)
				ctx.path = ctx.path[:len(ctx.path)-1]
			}
		case !fd.IsList() && fd.Kind() == protoreflect.MessageKind:
			child := v.Message()
			if child.IsValid() {
				ctx.path = append(ctx.path, frame{parent: parentName, field: string(fd.Name()), index: -1, child: string(child.Descriptor().Name())})
				walkInto(ctx, child, visit)
				ctx.path = ctx.path[:len(ctx.path)-1]
			}
		}
		return true
	})
}
