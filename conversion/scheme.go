package conversion

import (
	"errors"
	"fmt"
	"reflect"
)

type TypePair struct {
	Slice bool
	Arg   reflect.Type
	Out   reflect.Type
}

type ConversionFunc func(arg, out interface{}) error

type Scheme struct {
	convPairs map[TypePair]ConversionFunc
	ready     bool
}

func NewScheme() *Scheme {
	return &Scheme{
		convPairs: make(map[TypePair]ConversionFunc),
	}
}

func (s *Scheme) Register(arg, out interface{}, fn ConversionFunc) {
	if s.ready {
		panic("register too late!")
	}
	pair, err := getTypePair(arg, out)
	if err != nil {
		panic(err)
	}
	s.convPairs[pair] = fn
}

func (s *Scheme) ensureReady() {
	if !s.ready {
		panic("not ready")
	}
}

func invalidMessage(a0, b0 interface{}) string {
	if a0 == nil && b0 == nil {
		return fmt.Sprintf("no conversion")
	}
	return fmt.Sprintf("no conversion between (%T -> %T)", a0, b0)
}

func (s *Scheme) ConvertTo(args ...interface{}) error {
	s.ensureReady()
	c, a, b := s.validateConvertTo(args)
	if c == 0 {
		panic(invalidMessage(a, b))
	}
	return s.convertTo(args)
}

func (s *Scheme) ConvertChain(args ...interface{}) error {
	s.ensureReady()
	c, a, b := s.validateConvertTo(args)
	if c == 0 {
		panic(invalidMessage(a, b))
	}
	return s.convertChain(args)
}

//

//

func (s *Scheme) Convert(args ...interface{}) error {
	s.ensureReady()

	if len(args) == 2 {
		arg, out := args[0], args[1]
		fn := s.getConversion(arg, out)
		if fn == nil {
			panic(fmt.Sprintf("no conversion between (%T -> %T)", arg, out))
		}
		return fn(arg, out)
	}

	cc, a0, b0 := s.validateConvertChain(args)
	ct, a1, b1 := s.validateConvertTo(args)
	switch {
	case cc == 0 && ct == 0:
		if a0 == nil && b0 == nil {
			panic(fmt.Sprintf("no conversion"))
		}
		if a0 == a1 && b0 == b1 {
			panic(fmt.Sprintf("no conversion between (%T -> %T)", a0, b0))
		}
		panic(fmt.Sprintf("no conversion between (%T -> %T) and (%T -> %T)", a0, b0, a1, b1))

	case cc > 1 && ct > 1:
		panic(fmt.Sprintf("ambiguous conversions between (%T -> %T) and (%T -> %T)"+
			" (note: use ConvertTo or ConvertChain instead)", a0, b0, a1, b1))

	case cc == 1 && ct == 1:
		return s.convertChain(args)
	case cc != 0:
		return s.convertChain(args)
	case ct != 0:
		return s.convertTo(args)
	}
	return nil
}

func (s *Scheme) convertTo(args []interface{}) error {
	last := getLastNonFunc(args)
	for _, arg := range args {
		if arg == last {
			continue
		}
		switch fn := arg.(type) {
		case func():
			fn()
		case func() error:
			if err := fn(); err != nil {
				return err
			}
		default:
			if err := s.getConversion(arg, last)(arg, last); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Scheme) convertChain(args []interface{}) error {
	var prev interface{}
	for _, arg := range args {
		switch fn := arg.(type) {
		case func():
			fn()
		case func() error:
			if err := fn(); err != nil {
				return err
			}
		default:
			if prev != nil {
				if err := s.getConversion(prev, arg)(prev, arg); err != nil {
					return err
				}
			}
			prev = arg
		}
	}
	return nil
}

func getLastNonFunc(args []interface{}) interface{} {
	for i := len(args) - 1; i >= 0; i-- {
		switch args[i].(type) {
		case func(), func() error:
			continue
		}
		return args[i]
	}
	return nil
}

func (s *Scheme) validateConvertTo(args []interface{}) (count int, pairA, pairB interface{}) {
	last := getLastNonFunc(args)
	for _, arg := range args {
		switch arg.(type) {
		case func(), func() error:
			continue
		}
		if s.getConversion(arg, last) == nil {
			return 0, arg, last
		}
		if pairA == nil {
			pairA, pairB = arg, last
		}
		count++
	}
	return count, pairA, pairB
}

func (s *Scheme) validateConvertChain(args []interface{}) (count int, pairA, pairB interface{}) {
	var prev interface{}
	for _, arg := range args {
		switch arg.(type) {
		case func(), func() error:
			continue
		}
		if prev == nil {
			prev = arg
			continue
		}
		if s.getConversion(prev, arg) == nil {
			return 0, prev, arg
		}
		if pairA == nil {
			pairA, pairB = prev, arg
		}
		prev = arg
		count++
	}
	return count, pairA, pairB
}

func (s *Scheme) getConversion(arg, out interface{}) ConversionFunc {
	pair, err := getTypePair(arg, out)
	if err != nil {
		panic(fmt.Sprintf("invalid conversion type pair: %v (%T and %T)", err, arg, out))
	}
	fn := s.convPairs[pair]
	return fn
}

func getTypePair(arg, out interface{}) (TypePair, error) {
	argType := reflect.TypeOf(arg)
	outType := reflect.TypeOf(out)
	switch {
	case argType.Kind() == reflect.Slice && outType.Kind() == reflect.Slice:
		return TypePair{}, errors.New("second param must be pointer to slice")

	case argType.Kind() == reflect.Slice &&
		outType.Kind() == reflect.Ptr && outType.Elem().Kind() == reflect.Slice:
		if argType.Elem().Kind() == reflect.Ptr &&
			outType.Elem().Elem().Kind() == reflect.Ptr {
			pair := TypePair{
				Slice: true,
				Arg:   argType.Elem().Elem(),
				Out:   outType.Elem().Elem().Elem(),
			}
			return pair, nil
		}
		return TypePair{}, errors.New("must be slice of pointer")

	case argType.Kind() != reflect.Slice && outType.Kind() != reflect.Slice:
		if argType.Kind() == reflect.Ptr &&
			outType.Kind() == reflect.Ptr {
			pair := TypePair{
				Slice: false,
				Arg:   argType.Elem(),
				Out:   outType.Elem(),
			}
			return pair, nil
		}
		return TypePair{}, errors.New("must be pointer")

	default:
		return TypePair{}, errors.New("both types must match")
	}
}
