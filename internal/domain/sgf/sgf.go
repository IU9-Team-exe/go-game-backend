package sgf

// GameTree представляет одно дерево в SGF (узел + варианты)
type GameTree struct {
	Nodes    []Node      // Последовательность узлов (основная линия)
	Children []*GameTree // Варианты (вариативные линии)
}

// Node представляет один узел SGF (набор свойств, таких как B[pd], W[dd], C[...])
type Node struct {
	Properties map[string][]string // Свойства могут повторяться (например, AB[aa][bb])
}

// @name SGF
type SGF struct {
	Root *GameTree
}
