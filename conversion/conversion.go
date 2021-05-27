package conversion

func Build(funcs ...func(*Scheme)) *Scheme {
	s := NewScheme()
	for _, fn := range funcs {
		fn(s)
	}
	s.ready = true
	return s
}
