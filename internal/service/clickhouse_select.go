package service

import (
	"regexp"
	"strconv"
	"strings"
)

// This is the complete worksheet grammar, not a prefix check. Only parsed fields
// are used to construct SQL; the original text is never sent to the target.
var chSQLToken = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*|[0-9]+|'(?:[^'\\\x00-\x1f]|'')*'|[(),;=*]`)

type chSelect struct {
	table                         string
	columns                       []string
	countAlias                    string
	filter, literal, group, order string
	descending                    bool
	limit                         uint64
	hasLimit                      bool
}
type chTokens []string

func (p *chTokens) take(word string) bool {
	if len(*p) == 0 || !strings.EqualFold((*p)[0], word) {
		return false
	}
	*p = (*p)[1:]
	return true
}
func (p *chTokens) next() string {
	if len(*p) == 0 {
		return ""
	}
	s := (*p)[0]
	*p = (*p)[1:]
	return s
}
func (p *chTokens) ident() string {
	s := p.next()
	if !chIdentifier.MatchString(s) {
		return ""
	}
	return s
}
func parseCHSelect(sql string) (chSelect, error) {
	var q chSelect
	if len(sql) > 8192 {
		return q, ErrQueryValidationFailed
	}
	var p chTokens
	end := 0
	for _, m := range chSQLToken.FindAllStringIndex(sql, -1) {
		if strings.Trim(sql[end:m[0]], " \r\n\t") != "" || len(p) >= 64 {
			return q, ErrQueryValidationFailed
		}
		p = append(p, sql[m[0]:m[1]])
		end = m[1]
	}
	if strings.Trim(sql[end:], " \r\n\t") != "" || !p.take("select") {
		return q, ErrQueryValidationFailed
	}
	seen := map[string]bool{}
	for {
		name := p.ident()
		if name == "" {
			return q, ErrQueryValidationFailed
		}
		if strings.EqualFold(name, "count") && p.take("(") {
			p.take("*")
			if !p.take(")") || q.countAlias != "" {
				return q, ErrQueryValidationFailed
			}
			name = "count"
			if p.take("as") {
				name = p.ident()
			}
			q.countAlias = name
		}
		if name == "" || seen[name] || len(q.columns) == 8 {
			return q, ErrQueryValidationFailed
		}
		seen[name] = true
		q.columns = append(q.columns, name)
		if !p.take(",") {
			break
		}
	}
	if !p.take("from") {
		return q, ErrQueryValidationFailed
	}
	q.table = p.ident()
	if q.table == "" {
		return q, ErrQueryValidationFailed
	}
	if p.take("where") {
		q.filter = p.ident()
		if q.filter == "" || !p.take("=") {
			return q, ErrQueryValidationFailed
		}
		q.literal = p.next()
		if q.literal == "" || !(q.literal[0] == '\'' || (q.literal[0] >= '0' && q.literal[0] <= '9')) {
			return q, ErrQueryValidationFailed
		}
	}
	if p.take("group") {
		if !p.take("by") {
			return q, ErrQueryValidationFailed
		}
		q.group = p.ident()
		if q.group == "" || q.group == q.countAlias || !seen[q.group] || q.countAlias == "" {
			return q, ErrQueryValidationFailed
		}
	}
	if q.countAlias != "" {
		want := 1
		if q.group != "" {
			want++
		}
		if len(q.columns) != want {
			return q, ErrQueryValidationFailed
		}
	}
	if p.take("order") {
		if !p.take("by") {
			return q, ErrQueryValidationFailed
		}
		q.order = p.ident()
		if q.order == "" || !seen[q.order] {
			return q, ErrQueryValidationFailed
		}
		q.descending = p.take("desc")
		if !q.descending {
			p.take("asc")
		}
	}
	if p.take("limit") {
		var err error
		q.limit, err = strconv.ParseUint(p.next(), 10, 64)
		if err != nil {
			return q, ErrQueryValidationFailed
		}
		q.hasLimit = true
	}
	p.take(";")
	if len(p) != 0 {
		return q, ErrQueryValidationFailed
	}
	return q, nil
}
