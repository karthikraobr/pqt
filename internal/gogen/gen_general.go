package gogen

import (
	"fmt"

	"github.com/piotrkowalczuk/pqt"
	"github.com/piotrkowalczuk/pqt/internal/formatter"
	"github.com/piotrkowalczuk/pqt/internal/print"
	"github.com/piotrkowalczuk/pqt/pqtgo"
)

type Generator struct {
	print.Printer
	Plugins []Plugin
	Version float64
}

// Package generates package header.
func (g *Generator) Package(pkg string) {
	if pkg == "" {
		pkg = "main"
	}
	g.Printf("package %s\n", pkg)
}

func (g *Generator) Imports(s *pqt.Schema, fixed ...string) {
	imports := []string{
		"github.com/m4rw3r/uuid",
	}
	imports = append(imports, fixed...)

	appendIfNotEmpty := func(slice []string, elem string) []string {
		if elem != "" {
			return append(slice, elem)
		}
		return slice
	}
	for _, t := range s.Tables {
		for _, c := range t.Columns {
			if ct, ok := c.Type.(pqtgo.CustomType); ok {
				imports = appendIfNotEmpty(imports, ct.TypeOf(pqtgo.ModeMandatory).PkgPath())
				imports = appendIfNotEmpty(imports, ct.TypeOf(pqtgo.ModeOptional).PkgPath())
				imports = appendIfNotEmpty(imports, ct.TypeOf(pqtgo.ModeCriteria).PkgPath())
			}
		}
	}

	g.Println("import(")
	for _, imp := range imports {
		g.Print(`"`)
		g.Print(imp)
		g.Println(`"`)
	}
	g.Println(")")
}

func (g *Generator) Entity(t *pqt.Table) {
	g.Printf(`
// %sEntity ...`, formatter.Public(t.Name))
	g.Printf(`
type %sEntity struct{`, formatter.Public(t.Name))
	for prop := range g.entityPropertiesGenerator(t) {
		g.Printf(`
// %s ...`, formatter.Public(prop.Name))
		if prop.ReadOnly {
			g.Printf(`
// %s is read only`, formatter.Public(prop.Name))
		}
		if prop.Tags != "" {
			g.Printf(`
%s %s %s`, formatter.Public(prop.Name), prop.Type, prop.Tags)
		} else {
			g.Printf(`
%s %s`,
				formatter.Public(prop.Name),
				prop.Type,
			)
		}
	}
	g.Print(`}`)
}

func (g *Generator) Criteria(t *pqt.Table) {
	tableName := formatter.Public(t.Name)

	g.Printf(`
type %sCriteria struct {`, tableName)
	for _, c := range t.Columns {
		if t := g.columnType(c, pqtgo.ModeCriteria); t != "<nil>" {
			g.Printf(`
%s %s`, formatter.Public(c.Name), t)
		}
	}
	g.Printf(`
	operator string
	child, sibling, parent *%sCriteria
}`, tableName)
}

func (g *Generator) Operand(t *pqt.Table) {
	tableName := formatter.Public(t.Name)

	g.Printf(`
func %sOperand(operator string, operands ...*%sCriteria) *%sCriteria {
	if len(operands) == 0 {
		return &%sCriteria{operator: operator}
	}

	parent := &%sCriteria{
		operator: operator,
		child: operands[0],
	}

	for i := 0; i < len(operands); i++ {
		if i < len(operands)-1 {
			operands[i].sibling = operands[i+1]
		}
		operands[i].parent = parent
	}

	return parent
}`, tableName, tableName, tableName, tableName, tableName)
	g.Printf(`

func %sOr(operands ...*%sCriteria) *%sCriteria {
	return %sOperand("OR", operands...)
}`, tableName, tableName, tableName, tableName)
	g.Printf(`

func %sAnd(operands ...*%sCriteria) *%sCriteria {
	return %sOperand("AND", operands...)
}`, tableName, tableName, tableName, tableName)
}

func (g *Generator) Columns(t *pqt.Table) {
	g.Printf(`
const (
%s = "%s"`, formatter.Public("table", t.Name), t.FullName())

	for _, c := range t.Columns {
		g.Printf(`
%s = "%s"`, formatter.Public("table", t.Name, "column", c.Name), c.Name)
	}

	g.Printf(`
)

var %s = []string{`, formatter.Public("table", t.Name, "columns"))

	for _, c := range t.Columns {
		g.Printf(`
%s,`, formatter.Public("table", t.Name, "column", c.Name))
	}
	g.Print(`
}`)

}

func (g *Generator) Constraints(t *pqt.Table) {
	g.Printf(`
const (`)
	for _, c := range t.Constraints {
		name := pqt.JoinColumns(c.PrimaryColumns, "_")
		switch c.Type {
		case pqt.ConstraintTypeCheck:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraint", name, "Check"), c.String())
		case pqt.ConstraintTypePrimaryKey:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraintPrimaryKey"), c.String())
		case pqt.ConstraintTypeForeignKey:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraint", name, "ForeignKey"), c.String())
		case pqt.ConstraintTypeExclusion:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraint", name, "Exclusion"), c.String())
		case pqt.ConstraintTypeUnique:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraint", name, "Unique"), c.String())
		case pqt.ConstraintTypeIndex:
			g.Printf(`
%s = "%s"`, formatter.Public("table", c.PrimaryTable.Name, "constraint", name, "Index"), c.String())
		}
	}
	g.Printf(`
)`)
}

func (g *Generator) Repository(t *pqt.Table) {
	g.Printf(`
type %sRepositoryBase struct {
	%s string
	%s []string
	%s *sql.DB
	%s LogFunc
}`,
		formatter.Public(t.Name),
		formatter.Public("table"),
		formatter.Public("columns"),
		formatter.Public("db"),
		formatter.Public("log"),
	)
}

func (g *Generator) FindExpr(t *pqt.Table) {
	g.Printf(`
type %sFindExpr struct {`, formatter.Public(t.Name))
	g.Printf(`
%s *%sCriteria`, formatter.Public("where"), formatter.Public(t.Name))
	g.Printf(`
%s, %s int64`, formatter.Public("offset"), formatter.Public("limit"))
	g.Printf(`
%s []string`, formatter.Public("columns"))
	g.Printf(`
%s []RowOrder`, formatter.Public("orderBy"))
	for _, r := range joinableRelationships(t) {
		g.Printf(`
%s *%sJoin`, formatter.Public("join", or(r.InversedName, r.InversedTable.Name)), formatter.Public(r.InversedTable.Name))
	}
	g.Print(`
}`)
}

func (g *Generator) CountExpr(t *pqt.Table) {
	g.Printf(`
type %sCountExpr struct {`, formatter.Public(t.Name))
	g.Printf(`
%s *%sCriteria`, formatter.Public("where"), formatter.Public(t.Name))
	for _, r := range joinableRelationships(t) {
		g.Printf(`
%s *%sJoin`, formatter.Public("join", or(r.InversedName, r.InversedTable.Name)), formatter.Public(r.InversedTable.Name))
	}
	g.Print(`
}`)
}

func (g *Generator) Join(t *pqt.Table) {
	g.Printf(`
type %sJoin struct {`, formatter.Public(t.Name))
	g.Printf(`
%s, %s *%sCriteria`, formatter.Public("on"), formatter.Public("where"), formatter.Public(t.Name))
	g.Printf(`
%s bool`, formatter.Public("fetch"))
	g.Printf(`
%s JoinType`, formatter.Public("kind"))
	for _, r := range joinableRelationships(t) {
		g.Printf(`
Join%s *%sJoin`, formatter.Public(or(r.InversedName, r.InversedTable.Name)), formatter.Public(r.InversedTable.Name))
	}
	g.Print(`
}`)
}

func (g *Generator) Patch(t *pqt.Table) {
	g.Printf(`
type %sPatch struct {`, formatter.Public(t.Name))

ArgumentsLoop:
	for _, c := range t.Columns {
		if c.PrimaryKey {
			continue ArgumentsLoop
		}

		if t := g.columnType(c, pqtgo.ModeOptional); t != "<nil>" {
			g.Printf(`
%s %s`,
				formatter.Public(c.Name),
				t,
			)
		}
	}
	g.Print(`
}`)
}

func (g *Generator) Iterator(t *pqt.Table) {
	entityName := formatter.Public(t.Name)
	g.Printf(`
// %sIterator is not thread safe.
type %sIterator struct {
	rows Rows
	cols []string
	expr *%sFindExpr
}`, entityName,
		entityName,
		formatter.Public(t.Name))

	g.Printf(`
func (i *%sIterator) Next() bool {
	return i.rows.Next()
}

func (i *%sIterator) Close() error {
	return i.rows.Close()
}

func (i *%sIterator) Err() error {
	return i.rows.Err()
}

// Columns is wrapper around sql.Rows.Columns method, that also cache output inside iterator.
func (i *%sIterator) Columns() ([]string, error) {
	if i.cols == nil {
		cols, err := i.rows.Columns()
		if err != nil {
			return nil, err
		}
		i.cols = cols
	}
	return i.cols, nil
}

// Ent is wrapper around %s method that makes iterator more generic.
func (i *%sIterator) Ent() (interface{}, error) {
	return i.%s()
}

func (i *%sIterator) %s() (*%sEntity, error) {
	var ent %sEntity
	cols, err := i.Columns()
	if err != nil {
		return nil, err
	}

	props, err := ent.%s(cols...)
	if err != nil {
		return nil, err
	}`, entityName,
		entityName,
		entityName,
		entityName,
		entityName,
		entityName,
		formatter.Public(t.Name),
		entityName,
		formatter.Public(t.Name),
		entityName,
		entityName,
		formatter.Public("props"))

	if hasJoinableRelationships(t) {
		g.Print(`
		var prop []interface{}`)
	}
	g.scanJoinableRelationships(t, "i.expr")

	g.Print(`
	if err := i.rows.Scan(props...); err != nil {
		return nil, err
	}
	return &ent, nil
}`)
}

func (g *Generator) scanJoinableRelationships(t *pqt.Table, sel string) {
	for _, r := range joinableRelationships(t) {
		if r.Type == pqt.RelationshipTypeOneToMany || r.Type == pqt.RelationshipTypeManyToMany {
			continue
		}
		g.Printf(`
			if %s.%s != nil && %s.%s.%s {
				ent.%s = &%sEntity{}
				if prop, err = ent.%s.%s(); err != nil {
					return nil, err
				}
				props = append(props, prop...)
			}`,
			sel,
			formatter.Public("join", or(r.InversedName, r.InversedTable.Name)),
			sel,
			formatter.Public("join", or(r.InversedName, r.InversedTable.Name)),
			formatter.Public("fetch"),
			formatter.Public(or(r.InversedName, r.InversedTable.Name)),
			formatter.Public(r.InversedTable.Name),
			formatter.Public(or(r.InversedName, r.InversedTable.Name)),
			formatter.Public("props"),
		)
	}
}

// entityPropertiesGenerator produces struct field definition for each column and relationship defined on a table.
// It thread differently relationship differently based on ownership.
func (g *Generator) entityPropertiesGenerator(t *pqt.Table) chan structField {
	fields := make(chan structField)

	go func(out chan structField) {
		for _, c := range t.Columns {
			if t := g.columnType(c, pqtgo.ModeDefault); t != "<nil>" {
				out <- structField{Name: formatter.Public(c.Name), Type: t, ReadOnly: c.IsDynamic}
			}
		}

		for _, r := range t.OwnedRelationships {
			switch r.Type {
			case pqt.RelationshipTypeOneToMany:
				out <- structField{Name: formatter.Public(or(r.InversedName, r.InversedTable.Name+"s")), Type: fmt.Sprintf("[]*%sEntity", formatter.Public(r.InversedTable.Name))}
			case pqt.RelationshipTypeOneToOne:
				out <- structField{Name: formatter.Public(or(r.InversedName, r.InversedTable.Name)), Type: fmt.Sprintf("*%sEntity", formatter.Public(r.InversedTable.Name))}
			case pqt.RelationshipTypeManyToOne:
				out <- structField{Name: formatter.Public(or(r.InversedName, r.InversedTable.Name)), Type: fmt.Sprintf("*%sEntity", formatter.Public(r.InversedTable.Name))}
			}
		}

		for _, r := range t.InversedRelationships {
			switch r.Type {
			case pqt.RelationshipTypeOneToMany:
				out <- structField{Name: formatter.Public(or(r.OwnerName, r.OwnerTable.Name)), Type: fmt.Sprintf("*%sEntity", formatter.Public(r.OwnerTable.Name))}
			case pqt.RelationshipTypeOneToOne:
				out <- structField{Name: formatter.Public(or(r.OwnerName, r.OwnerTable.Name)), Type: fmt.Sprintf("*%sEntity", formatter.Public(r.OwnerTable.Name))}
			case pqt.RelationshipTypeManyToOne:
				out <- structField{Name: formatter.Public(or(r.OwnerName, r.OwnerTable.Name+"s")), Type: fmt.Sprintf("[]*%sEntity", formatter.Public(r.OwnerTable.Name))}
			}
		}

		for _, r := range t.ManyToManyRelationships {
			if r.Type != pqt.RelationshipTypeManyToMany {
				continue
			}

			switch {
			case r.OwnerTable == t:
				out <- structField{Name: formatter.Public(or(r.InversedName, r.InversedTable.Name+"s")), Type: fmt.Sprintf("[]*%sEntity", formatter.Public(r.InversedTable.Name))}
			case r.InversedTable == t:
				out <- structField{Name: formatter.Public(or(r.OwnerName, r.OwnerTable.Name+"s")), Type: fmt.Sprintf("[]*%sEntity", formatter.Public(r.OwnerTable.Name))}
			}
		}

		close(out)
	}(fields)

	return fields
}