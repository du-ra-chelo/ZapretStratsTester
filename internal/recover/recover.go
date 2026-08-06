package recover

import (
	"errors"
	"slices"
	"sync"
)

type Recoverer struct {
	mu    sync.Mutex
	funcs []func() error
}

func NewRecoverer() *Recoverer {
	return &Recoverer{
		funcs: make([]func() error, 0),
	}
}

func (r *Recoverer) Add(fn func() error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.funcs = append(r.funcs, fn)
}

func (r *Recoverer) RecoverAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error

	for _, f := range slices.Backward(r.funcs) {
		if err := f(); err != nil {
			errs = append(errs, err)
		}
	}
	r.funcs = nil
	return errors.Join(errs...)
}
