package sets

import "iter"

type Set[T comparable] interface {
	Add(T)
	Equals(Set[T]) bool
	EqualsSlice([]T) bool
	Iter() iter.Seq[T]
	Len() int
}

func FromSlice[T comparable](items []T) Set[T] {
	s := &set[T]{
		m: make(map[T]struct{}, len(items)),
	}

	for _, item := range items {
		s.Add(item)
	}

	return s
}

type set[T comparable] struct {
	m map[T]struct{}
}

func (s *set[T]) Add(t T) {
	s.m[t] = struct{}{}
}

func (s *set[T]) Equals(s2 Set[T]) bool {
	if s2.Len() != s.Len() {
		return false
	}

	for item := range s2.Iter() {
		if _, ok := s.m[item]; !ok {
			return false
		}
	}

	return true
}

func (s *set[T]) EqualsSlice(s2 []T) bool {
	if len(s2) != s.Len() {
		return false
	}

	for _, item := range s2 {
		if _, ok := s.m[item]; !ok {
			return false
		}
	}

	return true
}

func (s *set[T]) Iter() iter.Seq[T] {
	return func(yield func(T) bool) {
		for item := range s.m {
			if !yield(item) {
				return
			}
		}
	}
}

func (s *set[T]) Len() int {
	return len(s.m)
}

var _ Set[int] = (*set[int])(nil)
