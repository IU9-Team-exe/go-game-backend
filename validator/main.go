package validator

import (
	"errors"
	"fmt"
	"strings"
	"team_exe/internal/domain/game"
)

type Color int

const (
	Empty Color = iota
	Black
	White
)

func (c Color) String() string {
	switch c {
	case Black:
		return "Black"
	case White:
		return "White"
	default:
		return "Empty"
	}
}

var (
	ErrOffBoard = errors.New("move is off board")
	ErrOccupied = errors.New("point is already occupied")
	ErrSuicide  = errors.New("suicide move")
	ErrKo       = errors.New("move violates Ko rule")
)

// Move описывает один ход: координаты (X,Y), цвет и флаг паса.
type Move struct {
	X, Y  int
	Color Color
	Pass  bool
}

// Board хранит состояние доски размером size×size.
type Board struct {
	size int
	grid [][]Color
}

// NewBoard создаёт пустую доску заданного размера.
func NewBoard(size int) *Board {
	b := &Board{
		size: size,
		grid: make([][]Color, size),
	}
	for i := range b.grid {
		b.grid[i] = make([]Color, size)
	}
	return b
}

// Clone возвращает глубокую копию доски.
func (b *Board) Clone() *Board {
	nb := NewBoard(b.size)
	for x := 0; x < b.size; x++ {
		copy(nb.grid[x], b.grid[x])
	}
	return nb
}

// String возвращает строковую «карту» доски: '.' – пустая,
// 'X' – чёрная, 'O' – белая.
func (b *Board) String() string {
	var sb strings.Builder
	for y := 0; y < b.size; y++ {
		for x := 0; x < b.size; x++ {
			switch b.grid[x][y] {
			case Empty:
				sb.WriteByte('.')
			case Black:
				sb.WriteByte('X')
			case White:
				sb.WriteByte('O')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func opponent(c Color) Color {
	if c == Black {
		return White
	}
	if c == White {
		return Black
	}
	return Empty
}

type point struct{ X, Y int }

// neighbors возвращает ровно тех соседей (по сетке) точки (x,y),
// что находятся в пределах доски.
func (b *Board) neighbors(x, y int) []point {
	pts := []point{}
	if x > 0 {
		pts = append(pts, point{x - 1, y})
	}
	if x+1 < b.size {
		pts = append(pts, point{x + 1, y})
	}
	if y > 0 {
		pts = append(pts, point{x, y - 1})
	}
	if y+1 < b.size {
		pts = append(pts, point{x, y + 1})
	}
	return pts
}

// countLiberties обходит всю группу, к которой принадлежит камень в (x,y),
// считает её свободы и возвращает уникальное число свободных точек
// и список точек группы.
func (b *Board) countLiberties(x, y int) (int, []point) {
	color := b.grid[x][y]
	visited := map[point]struct{}{point{X: x, Y: y}: {}}
	stack := []point{{X: x, Y: y}}
	group := []point{{X: x, Y: y}}
	liberties := map[point]struct{}{}

	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, n := range b.neighbors(p.X, p.Y) {
			switch b.grid[n.X][n.Y] {
			case Empty:
				liberties[n] = struct{}{}
			case color:
				if _, seen := visited[n]; !seen {
					visited[n] = struct{}{}
					stack = append(stack, n)
					group = append(group, n)
				}
			}
		}
	}
	return len(liberties), group
}

// applyMove ставит камень m на доску и снимает все соседние вражеские группы
// без свобод.
func (b *Board) applyMove(m Move) {
	if m.Pass {
		return
	}
	b.grid[m.X][m.Y] = m.Color
	opp := opponent(m.Color)
	for _, n := range b.neighbors(m.X, m.Y) {
		if b.grid[n.X][n.Y] == opp {
			if libs, grp := b.countLiberties(n.X, n.Y); libs == 0 {
				for _, p := range grp {
					b.grid[p.X][p.Y] = Empty
				}
			}
		}
	}
}

// ValidateMove проверяет корректность хода m на доске size×size, учитывая
// историю history. Возвращает nil, если ход легален, иначе одну из ошибок:
// ErrOffBoard, ErrOccupied, ErrSuicide, ErrKo.
func ValidateMove(size int, history []Move, m Move) error {
	// Пас — всегда легально
	if m.Pass {
		return nil
	}
	// Проверка выхода за границы
	if m.X < 0 || m.X >= size || m.Y < 0 || m.Y >= size {
		return ErrOffBoard
	}
	// Восстанавливаем доску и собираем все предыдущие состояния
	b := NewBoard(size)
	states := map[string]struct{}{b.String(): {}}

	for i, mv := range history {
		if mv.Pass {
			continue
		}
		if mv.X < 0 || mv.X >= size || mv.Y < 0 || mv.Y >= size {
			return fmt.Errorf("invalid move in history at index %d: %v", i, mv)
		}
		if b.grid[mv.X][mv.Y] != Empty {
			return fmt.Errorf("invalid history: point occupied at %v", mv)
		}
		b.applyMove(mv)
		states[b.String()] = struct{}{}
	}
	// Проверка занятости
	if b.grid[m.X][m.Y] != Empty {
		return ErrOccupied
	}
	// Пробный ход на клоне
	nb := b.Clone()
	nb.applyMove(m)
	// Самоубийство
	if libs, _ := nb.countLiberties(m.X, m.Y); libs == 0 {
		return ErrSuicide
	}
	// Ко-правило: не повторять ни одно из предыдущих состояний
	newState := nb.String()
	if _, exists := states[newState]; exists {
		return ErrKo
	}
	return nil
}

func ParseOldMoves(oldMoves []game.Move, boardSize int) ([]Move, error) {
	var result []Move
	for i, om := range oldMoves {
		var m Move
		// Цвет
		switch strings.ToLower(om.Color) {
		case "black", "b":
			m.Color = Black
		case "white", "w":
			m.Color = White
		default:
			return nil, fmt.Errorf("invalid color at move %d: %s", i, om.Color)
		}
		// Пас
		if om.Coordinates == "" {
			m.Pass = true
			result = append(result, m)
			continue
		}
		// Координаты (например, "D4")
		colLetter := strings.ToUpper(om.Coordinates[:1])
		rowStr := om.Coordinates[1:]

		x := int(colLetter[0] - 'A')
		if colLetter >= "I" {
			x-- // В го пропускается 'I'
		}
		var y int
		_, err := fmt.Sscanf(rowStr, "%d", &y)
		if err != nil || y < 1 || y > boardSize {
			return nil, fmt.Errorf("invalid coordinate at move %d: %s", i, om.Coordinates)
		}
		// Преобразуем в (0,0)-ориентированные координаты
		m.X = x
		m.Y = boardSize - y
		result = append(result, m)
	}
	return result, nil
}

func ConvertOne(oldMove game.Move, boardSize int) (Move, error) {
	var m Move

	// Цвет
	switch strings.ToLower(oldMove.Color) {
	case "black":
		m.Color = Black
	case "white":
		m.Color = White
	default:
		return Move{}, fmt.Errorf("invalid color: %s", oldMove.Color)
	}

	// Пас
	if oldMove.Coordinates == "" {
		m.Pass = true
		return m, nil
	}

	// Координаты (например, "D4")
	colLetter := strings.ToUpper(oldMove.Coordinates[:1])
	rowStr := oldMove.Coordinates[1:]

	x := int(colLetter[0] - 'A')
	if colLetter >= "I" {
		x-- // В го пропускается 'I'
	}

	var y int
	_, err := fmt.Sscanf(rowStr, "%d", &y)
	if err != nil || y < 1 || y > boardSize {
		return Move{}, fmt.Errorf("invalid coordinate: %s", oldMove.Coordinates)
	}

	// Преобразуем в (0,0)-ориентированные координаты
	m.X = x
	m.Y = boardSize - y

	return m, nil
}
