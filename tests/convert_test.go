package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olvrng/ggen-convert/conversion"
)

var scheme = conversion.Build(RegisterConversions)

func TestConvert(t *testing.T) {
	t.Run("A to B", func(t *testing.T) {
		var b B
		a := &A{
			Value:   10,
			Int:     100,
			String:  "hello",
			Strings: []string{"one", "two"},

			C:   &C0{-10},
			Cs:  []*C0{{-100}, {-200}},
			D:   &D0{"first"},
			Ds:  []*D0{{"second"}, {"third"}},
			E:   E{"first"},
			Ep:  &E{"first"},
			Es:  []E{{"second"}, {"third"}},
			Eps: []*E{{"second"}, {"third"}},
		}
		err := scheme.Convert(a, &b)
		require.NoError(t, err)
		assert.Equal(t, b.Value, "10")
		assert.Equal(t, b.Int, int32(100))
		assert.Equal(t, b.String, S("hello"))
		assert.EqualValues(t, b.Strings, []string{"one", "two"})
		assert.Equal(t, b.C.Value, "-10")
		assert.EqualValues(t, b.Cs, []*C1{{"-100"}, {"-200"}})
		assert.Equal(t, b.D.Value, "first")
		assert.EqualValues(t, b.Ds, []*D1{{"second"}, {"third"}})
		assert.Equal(t, b.E.Value, "first")
		assert.Equal(t, b.Ep.Value, "first")
		assert.EqualValues(t, b.Es, []E{{"second"}, {"third"}})
		assert.EqualValues(t, b.Eps, []*E{{"second"}, {"third"}})
	})
	t.Run("[]*A to []*B", func(t *testing.T) {
		var bs []*B
		as := []*A{{Value: 10}, {Value: 20}}
		err := scheme.Convert(as, &bs)
		require.NoError(t, err)
		require.Len(t, bs, 2)
		require.Equal(t, bs[0].Value, "10")
		require.Equal(t, bs[1].Value, "20")
	})
	t.Run("Embedded", func(t *testing.T) {
		t.Run("C0 to C2", func(t *testing.T) {
			from := &C0{100}
			var to C2
			err := scheme.Convert(from, &to)
			require.NoError(t, err)
			assert.Equal(t, 100, to.Value)
		})
		t.Run("C2 to C0", func(t *testing.T) {
			from := &C2{C0: C0{100}}
			var to C0
			err := scheme.Convert(from, &to)
			require.NoError(t, err)
			assert.Equal(t, 100, to.Value)
		})
		t.Run("C0 to C3 (pointer)", func(t *testing.T) {
			from := &C0{100}
			var to C3
			err := scheme.Convert(from, &to)
			require.NoError(t, err)
			assert.Equal(t, 100, to.Value)
		})
		t.Run("C3 to C0 (pointer)", func(t *testing.T) {
			from := &C3{C0: &C0{100}}
			var to C0
			err := scheme.Convert(from, &to)
			require.NoError(t, err)
			assert.Equal(t, 100, to.Value)
		})
	})
}
