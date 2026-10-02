//go:build !windows

package input

import "errors"

type Injector struct{}

func NewInjector() *Injector {
	return &Injector{}
}

func (inj *Injector) RefreshMetrics() {}

func (inj *Injector) HandleInput(msg *InputMessage) error {
	return errors.New("input injection is only supported on Windows")
}
