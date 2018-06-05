package pqtsql

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/piotrkowalczuk/pqt"
)

// Generator ...
type Generator struct {
	// Version represents version of Postgres database generated code will run against.
	Version float64
}

// Generate generates code based on given schema.
func (g *Generator) Generate(s *pqt.Schema) ([]byte, error) {
	code, err := g.generate(s)
	if err != nil {
		return nil, err
	}

	return code.Bytes(), nil
}

// GenerateTo works like Generate, but writes directly into io.Writer.
func (g *Generator) GenerateTo(s *pqt.Schema, w io.Writer) error {
	code, err := g.generate(s)
	if err != nil {
		return err
	}

	_, err = code.WriteTo(w)
	return err
}

func (g *Generator) generate(s *pqt.Schema) (*bytes.Buffer, error) {
	code := bytes.NewBufferString("-- do not modify, generated by pqt\n\n")
	if s.Name != "" {
		fmt.Fprint(code, "CREATE SCHEMA ")
		if s.IfNotExists {
			fmt.Fprint(code, "IF NOT EXISTS ")
		}
		fmt.Fprintf(code, "%s; \n\n", s.Name)
	}
	for _, f := range s.Functions {
		if err := g.generateCreateFunction(code, f); err != nil {
			return nil, err
		}
	}
	for _, t := range s.Tables {
		if err := g.generateCreateTable(code, t); err != nil {
			return nil, err
		}
		for _, cnstr := range t.Constraints {
			switch cnstr.Type {
			case pqt.ConstraintTypeIndex:
				indexConstraintQuery(code, cnstr, g.Version)
			case pqt.ConstraintTypeUniqueIndex:
				uniqueIndexConstraintQuery(code, cnstr, g.Version)
			}
		}
		fmt.Fprintln(code, "")
	}

	return code, nil
}

func (g *Generator) generateCreateFunction(buf *bytes.Buffer, f *pqt.Function) error {
	if f == nil {
		return nil
	}
	if f.Name == "" {
		return errors.New("missing function name")
	}

	buf.WriteString("CREATE OR REPLACE FUNCTION ")
	buf.WriteString(f.Name)
	buf.WriteString("(")
	for i, arg := range f.Args {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(arg.Name)
		buf.WriteString(" ")
		buf.WriteString(arg.Type.String())
	}
	buf.WriteString(") RETURNS ")
	buf.WriteString(f.Type.String())
	buf.WriteString("\n	AS '")
	buf.WriteString(f.Body)
	buf.WriteString("'\n	LANGUAGE SQL")
	switch f.Behaviour {
	case pqt.FunctionBehaviourVolatile:
		buf.WriteString("\n	VOLATILE")
	case pqt.FunctionBehaviourImmutable:
		buf.WriteString("\n	IMMUTABLE")
	case pqt.FunctionBehaviourStable:
		buf.WriteString("\n	STABLE")
	}
	buf.WriteString(";\n\n")

	return nil
}

func (g *Generator) generateCreateTable(buf *bytes.Buffer, t *pqt.Table) error {
	if t == nil {
		return nil
	}

	if t.Name == "" {
		return errors.New("missing table name")
	}
	if len(t.Columns) == 0 {
		return fmt.Errorf("table %s has no columns", t.Name)
	}

	buf.WriteString("CREATE ")
	if t.Temporary {
		buf.WriteString("TEMPORARY ")
	}
	buf.WriteString("TABLE ")
	if t.IfNotExists {
		buf.WriteString("IF NOT EXISTS ")
	}
	if t.Schema != nil {
		buf.WriteString(t.Schema.Name)
		buf.WriteRune('.')
		buf.WriteString(t.Name)
	} else {
		buf.WriteString(t.Name)
	}
	buf.WriteString(" (\n")

	constraints := t.Constraints
	for _, r := range t.OwnedRelationships {
		// If ...
		if len(r.OwnerColumns) == 1 {
			continue
		}
		if r.OwnerForeignKey != nil {
			constraints = append(constraints, r.OwnerForeignKey)
		}
	}
	nbOfConstraints := constraints.CountOf(
		pqt.ConstraintTypePrimaryKey,
		pqt.ConstraintTypeCheck,
		pqt.ConstraintTypeUnique,
		pqt.ConstraintTypeForeignKey,
		pqt.ConstraintTypeExclusion,
	)
	for i, c := range t.Columns {
		if c.IsDynamic {
			continue
		}
		buf.WriteRune('	')
		buf.WriteString(c.Name)
		buf.WriteRune(' ')
		buf.WriteString(c.Type.String())
		if c.Collate != "" {
			buf.WriteRune(' ')
			buf.WriteString(c.Collate)
		}
		if d, ok := c.DefaultOn(pqt.EventInsert); ok {
			buf.WriteString(" DEFAULT ")
			buf.WriteString(d)
		}
		if c.NotNull {
			buf.WriteString(" NOT NULL")
		}

		if i < len(t.Columns)-1 || nbOfConstraints > 0 {
			buf.WriteRune(',')
		}
		buf.WriteRune('\n')
	}

	if nbOfConstraints > 0 {
		buf.WriteRune('\n')
	}

	i := 0
	for _, c := range constraints {
		if c.Type == pqt.ConstraintTypeIndex || c.Type == pqt.ConstraintTypeUniqueIndex {
			continue
		}
		buf.WriteString("	")
		err := g.generateConstraint(buf, c)
		if err != nil {
			return err
		}
		if i < nbOfConstraints-1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('\n')
		i++
	}

	buf.WriteString(");\n")

	return nil
}

func (g *Generator) generateConstraint(buf *bytes.Buffer, c *pqt.Constraint) error {
	switch c.Type {
	case pqt.ConstraintTypeUnique:
		uniqueConstraintQuery(buf, c)
	case pqt.ConstraintTypePrimaryKey:
		primaryKeyConstraintQuery(buf, c)
	case pqt.ConstraintTypeForeignKey:
		return foreignKeyConstraintQuery(buf, c)
	case pqt.ConstraintTypeCheck:
		checkConstraintQuery(buf, c)
	case pqt.ConstraintTypeIndex:
	case pqt.ConstraintTypeUniqueIndex:
	default:
		return fmt.Errorf("unknown constraint type: %s", c.Type)
	}

	return nil
}

func uniqueConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint) {
	fmt.Fprintf(buf, `CONSTRAINT "%s" UNIQUE (%s)`, c.Name(), pqt.JoinColumns(c.PrimaryColumns, ", "))
}

func primaryKeyConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint) {
	fmt.Fprintf(buf, `CONSTRAINT "%s" PRIMARY KEY (%s)`, c.Name(), pqt.JoinColumns(c.PrimaryColumns, ", "))
}

func foreignKeyConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint) error {
	switch {
	case len(c.PrimaryColumns) == 0:
		return errors.New("foreign key constraint require at least one column")
	case len(c.Columns) == 0:
		return errors.New("foreign key constraint require at least one reference column")
	case c.Table == nil:
		return errors.New("foreiqn key constraint missing reference table")
	}

	fmt.Fprintf(buf, `CONSTRAINT "%s" FOREIGN KEY (%s) REFERENCES %s (%s)`,
		c.Name(),
		pqt.JoinColumns(c.PrimaryColumns, ", "),
		c.Table.FullName(),
		pqt.JoinColumns(c.Columns, ", "),
	)

	switch c.OnDelete {
	case pqt.Cascade:
		buf.WriteString(" ON DELETE CASCADE")
	case pqt.Restrict:
		buf.WriteString(" ON DELETE RESTRICT")
	case pqt.SetNull:
		buf.WriteString(" ON DELETE SET NULL")
	case pqt.SetDefault:
		buf.WriteString(" ON DELETE SET DEFAULT")
	}

	switch c.OnUpdate {
	case pqt.Cascade:
		buf.WriteString(" ON UPDATE CASCADE")
	case pqt.Restrict:
		buf.WriteString(" ON UPDATE RESTRICT")
	case pqt.SetNull:
		buf.WriteString(" ON UPDATE SET NULL")
	case pqt.SetDefault:
		buf.WriteString(" ON UPDATE SET DEFAULT")
	}

	return nil
}

func checkConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint) {
	fmt.Fprintf(buf, `CONSTRAINT "%s" CHECK (%s)`, c.Name(), c.Check)
}

func indexConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint, ver float64) {
	// TODO: change code so IF NOT EXISTS is optional
	if ver >= 9.5 {
		fmt.Fprintf(buf, `CREATE INDEX IF NOT EXISTS "%s" ON %s (%s);`, c.Name(), c.PrimaryTable.FullName(), c.PrimaryColumns.String())
	} else {
		fmt.Fprintf(buf, `CREATE INDEX "%s" ON %s (%s);`, c.Name(), c.PrimaryTable.FullName(), c.PrimaryColumns.String())
	}
	fmt.Fprintln(buf, "")
}

func uniqueIndexConstraintQuery(buf *bytes.Buffer, c *pqt.Constraint, ver float64) {
	if ver >= 9.5 {
		fmt.Fprintf(buf, `CREATE UNIQUE INDEX IF NOT EXISTS "%s" ON %s (%s)`, c.Name(), c.PrimaryTable.FullName(), c.PrimaryColumns.String())
	} else {
		fmt.Fprintf(buf, `CREATE UNIQUE INDEX "%s" ON %s (%s)`, c.Name(), c.PrimaryTable.FullName(), c.PrimaryColumns.String())
	}
	if c.Where != "" {
		fmt.Fprintf(buf, " WHERE %s", c.Where)
	}

	fmt.Fprint(buf, ";\n")
}
