package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

// SchemaLookup retrieves an introspected table by name. It is consulted by
// the SQL builder when expanding embedded relations.
type SchemaLookup interface {
	Table(name string) (*schema.Table, bool)
}

type relDir int

const (
	relOneToMany relDir = iota + 1
	relManyToOne
)

type relation struct {
	dir        relDir
	parentKeys []string // columns on the parent table participating in the join
	childKeys  []string // columns on the child table participating in the join
}

// resolveRelation finds a foreign-key path between parent and child. It first
// checks whether the parent itself has an FK pointing at child (many-to-one),
// and otherwise looks for an FK on child that points back at parent
// (one-to-many). Multiple FKs between the same pair are reported as
// ambiguous so the operator can disambiguate with explicit syntax later.
//
// ForeignKey.Table is populated by the schema introspector with a
// "schema.name" qualified name (see schema.qualify), so we compare against
// the same qualified form rather than just Table.Name.
func resolveRelation(parent, child *schema.Table) (relation, error) {
	parentQual := qualifiedTableName(parent)
	childQual := qualifiedTableName(child)

	var manyToOne *schema.ForeignKey

	for i, fk := range parent.ForeignKeys {
		if fk.Table != childQual {
			continue
		}

		if manyToOne != nil {
			return relation{}, fmt.Errorf(
				"ambiguous relation: multiple foreign keys from %q to %q", parent.Name, child.Name)
		}

		manyToOne = &parent.ForeignKeys[i]
	}

	var oneToMany *schema.ForeignKey

	for i, fk := range child.ForeignKeys {
		if fk.Table != parentQual {
			continue
		}

		if oneToMany != nil {
			return relation{}, fmt.Errorf(
				"ambiguous relation: multiple foreign keys from %q to %q", child.Name, parent.Name)
		}

		oneToMany = &child.ForeignKeys[i]
	}

	if manyToOne != nil && oneToMany != nil {
		return relation{}, fmt.Errorf(
			"ambiguous relation: foreign keys exist in both directions between %q and %q",
			parent.Name, child.Name)
	}

	if manyToOne != nil {
		return relation{dir: relManyToOne, parentKeys: manyToOne.Columns, childKeys: manyToOne.References}, nil
	}

	if oneToMany != nil {
		return relation{dir: relOneToMany, parentKeys: oneToMany.References, childKeys: oneToMany.Columns}, nil
	}

	return relation{}, fmt.Errorf("no foreign key relation between %q and %q", parent.Name, child.Name)
}

func qualifiedTableName(t *schema.Table) string {
	if t.Schema == "" {
		return t.Name
	}

	return t.Schema + "." + t.Name
}

// buildEmbedSubquery emits the SELECT alias clause for an embedded relation.
// One-to-many produces a JSON-aggregated correlated subquery; many-to-one
// produces a JSON_OBJECT subquery with LIMIT 1. Nested embeds are rejected.
func buildEmbedSubquery(
	item query.SelectItem,
	parent *schema.Table,
	lookup SchemaLookup,
	d dialect.Dialect,
) (string, error) {
	if item.Embed == nil {
		return "", errors.New("internal: buildEmbedSubquery called on non-embed item")
	}

	child, ok := lookup.Table(item.Embed.Relation)
	if !ok {
		return "", fmt.Errorf("unknown relation %q from table %q", item.Embed.Relation, parent.Name)
	}

	for _, sub := range item.Embed.Items {
		if sub.Embed != nil {
			return "", fmt.Errorf("nested embed is not supported (in relation %q)", item.Embed.Relation)
		}
	}

	rel, err := resolveRelation(parent, child)
	if err != nil {
		return "", err
	}

	fieldNames, err := embedFieldNames(item.Embed.Items, child)
	if err != nil {
		return "", err
	}

	childTableQuoted := d.Quote(child.Name)
	parentTableQuoted := d.Quote(parent.Name)

	pairs := make([]string, 0, len(fieldNames)*2)
	for _, name := range fieldNames {
		pairs = append(pairs, d.StringLiteral(name))
		pairs = append(pairs, childTableQuoted+"."+d.Quote(name))
	}

	objExpr := d.JSONObject(pairs)
	childTableExpr := qualifiedTable(child, d)

	onParts := make([]string, 0, len(rel.parentKeys))
	for i := range rel.parentKeys {
		onParts = append(onParts, fmt.Sprintf("%s.%s = %s.%s",
			childTableQuoted, d.Quote(rel.childKeys[i]),
			parentTableQuoted, d.Quote(rel.parentKeys[i])))
	}

	on := strings.Join(onParts, " AND ")

	var sub string
	if rel.dir == relOneToMany {
		agg := d.JSONAgg(objExpr, "")
		sub = fmt.Sprintf("(SELECT COALESCE(%s, %s) FROM %s WHERE %s)",
			agg, d.EmptyJSONArray(), childTableExpr, on)
	} else {
		sub = fmt.Sprintf("(SELECT %s FROM %s WHERE %s LIMIT 1)",
			objExpr, childTableExpr, on)
	}

	alias := item.Alias
	if alias == "" {
		alias = item.Embed.Relation
	}

	return sub + " AS " + d.Quote(alias), nil
}

// embedFieldNames produces the deduplicated, validated list of column names
// to include in an embedded JSON object.
func embedFieldNames(items []query.SelectItem, child *schema.Table) ([]string, error) {
	childCols := columnSet(child)

	if len(items) == 0 {
		names := make([]string, 0, len(child.Columns))
		for _, c := range child.Columns {
			names = append(names, c.Name)
		}

		return names, nil
	}

	var hasStar, hasNamed bool

	seen := make(map[string]struct{}, len(items))
	names := make([]string, 0, len(items))

	for _, it := range items {
		if it.Column == "*" {
			if hasStar {
				return nil, fmt.Errorf("embed '%s': '*' specified more than once", child.Name)
			}

			hasStar = true

			for _, c := range child.Columns {
				if _, dup := seen[c.Name]; dup {
					continue
				}

				seen[c.Name] = struct{}{}
				names = append(names, c.Name)
			}

			continue
		}

		hasNamed = true

		if _, ok := childCols[it.Column]; !ok {
			return nil, fmt.Errorf("unknown column %q on table %q", it.Column, child.Name)
		}

		if _, dup := seen[it.Column]; dup {
			return nil, fmt.Errorf("duplicate select column %q in embed %q", it.Column, child.Name)
		}

		seen[it.Column] = struct{}{}
		names = append(names, it.Column)
	}

	if hasStar && hasNamed {
		return nil, fmt.Errorf("embed %q cannot mix '*' with named columns", child.Name)
	}

	return names, nil
}
