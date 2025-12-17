// Package ct provides Category Theory primitives for composable code generation.
package ct

// Monoid defines an algebraic structure with identity and associative append.
type Monoid[A any] struct {
	Empty  func() A
	Append func(A, A) A
}

// Map applies a function to each element of a slice.
func Map[A, B any](xs []A, f func(A) B) []B {
	result := make([]B, len(xs))
	for i, x := range xs {
		result[i] = f(x)
	}
	return result
}

// Filter returns elements that satisfy the predicate.
func Filter[A any](xs []A, pred func(A) bool) []A {
	result := make([]A, 0, len(xs))
	for _, x := range xs {
		if pred(x) {
			result = append(result, x)
		}
	}
	return result
}

// Concat combines a slice of values using the monoid.
func Concat[A any](m Monoid[A], xs []A) A {
	result := m.Empty()
	for _, x := range xs {
		result = m.Append(result, x)
	}
	return result
}

// FoldMap maps and then folds in one pass.
func FoldMap[A, B any](xs []A, m Monoid[B], f func(A) B) B {
	result := m.Empty()
	for _, x := range xs {
		result = m.Append(result, f(x))
	}
	return result
}

// FoldMapIndexed is like FoldMap but provides the index.
func FoldMapIndexed[A, B any](xs []A, m Monoid[B], f func(int, A) B) B {
	result := m.Empty()
	for i, x := range xs {
		result = m.Append(result, f(i, x))
	}
	return result
}

// StringMonoid concatenates strings.
var StringMonoid = Monoid[string]{
	Empty:  func() string { return "" },
	Append: func(a, b string) string { return a + b },
}

// SliceMonoid appends slices.
func SliceMonoid[A any]() Monoid[[]A] {
	return Monoid[[]A]{
		Empty:  func() []A { return nil },
		Append: func(a, b []A) []A { return append(a, b...) },
	}
}

// Unique removes duplicates from a slice.
func Unique[A comparable](xs []A) []A {
	seen := make(map[A]bool)
	result := make([]A, 0, len(xs))
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			result = append(result, x)
		}
	}
	return result
}

// Coalesce returns the first non-zero value.
func Coalesce[A comparable](values ...A) A {
	var zero A
	for _, v := range values {
		if v != zero {
			return v
		}
	}
	return zero
}
