// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package taskgroup

import (
	"sync"

	"go.uber.org/multierr"
)

// Task is a function that returns an error.
// See `taskgroup.New`.
type Task func() error

// Filter is a function that takes an error and returns it (or a modified error)
// if it should be retained by `TaskGroup.Wait`.
type Filter func(error) error

// ErrorGroup is an error that contains a list of errors.
// This interface is implemented by `multierr.multiError`
type ErrorGroup interface {
	Errors() []error
	Error() string
}

// TaskGroup is a helper to make it easy to execute one or more `Task` functions
// and collect their errors, if any.
type TaskGroup struct {
	errCh  chan error
	wg     sync.WaitGroup
	filter Filter
}

// New returns a new TaskGroup, which can be used to execute one or more `Task`
// functions and collect their errors, if any.
func New() *TaskGroup {
	return &TaskGroup{
		errCh: make(chan error),
	}
}

// NewWithFilter returns a new TaskGroup, like `New`, but with a filter for
// optionally ignoring/muting errors using a `Filter` function.
func NewWithFilter(f Filter) *TaskGroup {
	return &TaskGroup{
		errCh:  make(chan error),
		filter: f,
	}
}

// Go starts a `Task` in a background goroutine.
func (tg *TaskGroup) Go(t Task) {
	tg.wg.Add(1)
	go func() {
		defer tg.wg.Done()
		tg.errCh <- t()
	}()
}

// Wait blocks until all tasks are complete and returns a MultiError of their
// errors, if any. If the TaskGroup was created with a Filter, the filter will
// be called for each non-nil Task error, and only the non-nil error results
// will be returned.
// If multiple tasks errored (and more than one was not filtered), an
// `ErrorGroup` will be returned. Otherwise, returns the only error, or nil.
func (tg *TaskGroup) Wait() error {
	go func() {
		defer close(tg.errCh)
		tg.wg.Wait()
	}()
	var errs []error
	for err := range tg.errCh {
		if err != nil && tg.filter != nil {
			err = tg.filter(err)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return multierr.Combine(errs...)
}
