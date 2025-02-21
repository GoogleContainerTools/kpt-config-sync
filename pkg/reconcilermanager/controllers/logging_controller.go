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

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
)

// contextKey is a unique type to avoid overlap with other string context keys
type contextKey string

// String returns the context key and package path
func (c contextKey) String() string {
	return fmt.Sprintf("context key (pkg/reconcilermanager/controllers): %s", string(c))
}

// contextKeyLogger is used to store the current logr.Logger, with contextual
// values, in the context.Context.
const contextKeyLogger = contextKey("logger")

// LoggingController is a parent class for a controller that logs.
// The logger can be stored in the context with contextual values.
//
// Use lc.logger(ctx) to retrieve the logger.
// Use ctx = lc.setLoggerValues(ctx, key, value) to add key/value pairs.
type LoggingController struct {
	log logr.Logger
}

// NewLoggingController constructs a new LoggingController
func NewLoggingController(log logr.Logger) *LoggingController {
	return &LoggingController{
		log: log,
	}
}

// Logger returns a logr.Logger, either from the context or from
// reconcilerBase.log.
func (lc *LoggingController) Logger(ctx context.Context) logr.Logger {
	logger := ctx.Value(contextKeyLogger)
	if logger != nil {
		return logger.(logr.Logger)
	}
	return lc.log
}

// SetLoggerValues sets key/value pairs on the logger stored in the context.
// If not initially present, the default logger is added to the context.
// See logr.Logger.WithValues for more details about how values work.
func (lc *LoggingController) SetLoggerValues(ctx context.Context, keysAndValues ...interface{}) context.Context {
	return context.WithValue(ctx, contextKeyLogger, lc.Logger(ctx).WithValues(keysAndValues...))
}
