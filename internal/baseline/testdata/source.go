package fixture

var count = 1

type Counter int

func (c *Counter) Add() {
	*c += Counter(count)
}

type Pair[A, B any] struct{}

func (p *Pair[A, B]) Reset() {}

func Outer() func() int {
	return func() int {
		return count
	}
}
