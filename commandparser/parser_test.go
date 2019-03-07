package parser

import (
	"fmt"
	"testing"
)

func TestParser(t *testing.T) {
	p := New()

	p.On("hello", "says hello", func(c C) {
		fmt.Println("hello", c.Float64(0))
	})

	p.Parse("testzone", "tester", ".hello 6.9")
}
