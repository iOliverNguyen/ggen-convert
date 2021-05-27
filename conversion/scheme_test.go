package conversion

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type A struct {
	Value int
}

type B struct {
	Value string
}

func TestScheme(t *testing.T) {
	s := NewScheme()
	s.Register((*A)(nil), (*B)(nil), func(a, b interface{}) error {
		v := a.(*A).Value
		b.(*B).Value = strconv.Itoa(v)
		return nil
	})
	s.Register((*B)(nil), (*A)(nil), func(b, a interface{}) (err error) {
		v := b.(*B).Value
		a.(*A).Value, err = strconv.Atoi(v)
		return err
	})
	s.Register(([]*A)(nil), (*[]*B)(nil), func(as, bs interface{}) error {
		args := as.([]*A)
		outs := make([]*B, len(args))
		for i := range args {
			outs[i] = &B{strconv.Itoa(args[i].Value)}
		}
		*bs.(*[]*B) = outs
		return nil
	})
	s.ready = true

	t.Run("A to B", func(t *testing.T) {
		var b B
		err := s.Convert(&A{10}, &b)
		require.NoError(t, err)
		require.Equal(t, b.Value, "10")
	})
	t.Run("B to A", func(t *testing.T) {
		var a A
		err := s.Convert(&B{"10"}, &a)
		require.NoError(t, err)
		require.Equal(t, a.Value, 10)
	})
	t.Run("[]*A to []*B", func(t *testing.T) {
		var bs []*B
		as := []*A{{10}, {20}}
		err := s.Convert(as, &bs)
		require.NoError(t, err)
		require.Len(t, bs, 2)
		require.Equal(t, bs[0].Value, "10")
		require.Equal(t, bs[1].Value, "20")
	})
}
