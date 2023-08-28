// Copyright 2022 Google LLC
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

package bugreport

import (
	"context"
	"fmt"
	"io"
)

// convertibleLogSourceIdentifiers is an interface for mocking the logSource type in unit tests
type convertibleLogSourceIdentifiers interface {
	fetchRcForLogSource(context.Context, coreClient) (io.ReadCloser, error)
	pathName() string
}

type logSources []convertibleLogSourceIdentifiers

func (ls logSources) convertLogSourcesToReadables(ctx context.Context, cs coreClient) ([]Readable, []error) {
	var rs []Readable
	var errorList []error

	for _, l := range ls {
		rc, err := l.fetchRcForLogSource(ctx, cs)
		if err != nil {
			e := fmt.Errorf("failed to create reader for logSource: %v", err)
			errorList = append(errorList, e)
			continue
		}

		rs = append(rs, Readable{
			ReadCloser: rc,
			Name:       fmt.Sprintf("%s.log", l.pathName()),
		})
	}

	return rs, errorList
}
