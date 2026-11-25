package recipe

// Cell is one box of the table. Row/Col are zero-based grid positions;
// spans are at least 1. Ingredients sit in column 0; a Step sits one
// column right of its deepest input; every cell stretches right until
// the column of the Step that consumes it, so the grid is fully tiled.
type Cell struct {
	Ingredient   *Ingredient // exactly one of Ingredient / Step is set
	Step         *Step
	Row, RowSpan int
	Col, ColSpan int
}

// Layout is the grid derived from a valid Tree (ADR 0002). Cells are in
// drawing order: ingredients top to bottom, then steps in the order they
// happen (every input before the step that consumes it).
type Layout struct {
	Rows, Cols int
	Cells      []Cell
}

// Lay computes the Layout. r must have passed Validate.
//
// Rows are the leaves in depth-first input order from the Root, which
// makes the leaves under any Step contiguous — so every valid Tree draws
// without crossings and the agent controls row order by input order.
func Lay(r *Recipe) Layout {
	steps := r.stepIndex()
	ings := make(map[string]*Ingredient, len(r.Ingredients))
	for i := range r.Ingredients {
		ings[r.Ingredients[i].ID] = &r.Ingredients[i]
	}

	type span struct{ row, rows, col int }
	pos := map[string]span{}
	var ingCells, stepCells []Cell
	parentCol := map[string]int{}
	row := 0

	var visit func(id string) span
	visit = func(id string) span {
		if in, ok := ings[id]; ok {
			sp := span{row: row, rows: 1, col: 0}
			row++
			pos[id] = sp
			ingCells = append(ingCells, Cell{Ingredient: in, Row: sp.row, RowSpan: 1, Col: 0})
			return sp
		}
		s := steps[id]
		first, last, deep := -1, -1, 0
		for _, in := range s.Inputs {
			c := visit(in)
			if first < 0 {
				first = c.row
			}
			last = c.row + c.rows - 1
			if c.col > deep {
				deep = c.col
			}
		}
		sp := span{row: first, rows: last - first + 1, col: deep + 1}
		for _, in := range s.Inputs {
			parentCol[in] = sp.col
		}
		pos[id] = sp
		stepCells = append(stepCells, Cell{Step: s, Row: sp.row, RowSpan: sp.rows, Col: sp.col})
		return sp
	}
	root := r.Root()
	rootSpan := visit(root.ID)

	l := Layout{Rows: row, Cols: rootSpan.col + 1}
	for _, c := range append(ingCells, stepCells...) {
		id := ""
		if c.Ingredient != nil {
			id = c.Ingredient.ID
		} else {
			id = c.Step.ID
		}
		if pc, ok := parentCol[id]; ok {
			c.ColSpan = pc - c.Col
		} else {
			c.ColSpan = 1 // the Root
		}
		l.Cells = append(l.Cells, c)
	}
	return l
}

// StepsInOrder lists Steps so every input comes before its consumer —
// the order the plain-text fallback reads them in.
func StepsInOrder(r *Recipe) []*Step {
	var out []*Step
	for _, c := range Lay(r).Cells {
		if c.Step != nil {
			out = append(out, c.Step)
		}
	}
	return out
}
