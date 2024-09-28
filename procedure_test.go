// Copyright 2024 Buf Technologies, Inc.
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

package pluginrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcedureBasic(t *testing.T) {
	t.Parallel()

	procedure, err := NewProcedure("/foo/bar")
	require.NoError(t, err)
	require.Equal(t, "/foo/bar", procedure.Path())
	require.Empty(t, procedure.Args())

	procedure, err = NewProcedure("/foo/bar", ProcedureWithArgs("foo", "bar"))
	require.NoError(t, err)
	require.Equal(t, "/foo/bar", procedure.Path())
	require.Equal(t, []string{"foo", "bar"}, procedure.Args())

	_, err = NewProcedure("foo/bar")
	require.Error(t, err)
	_, err = NewProcedure("\\foo\\bar")
	require.Error(t, err)
	_, err = NewProcedure("/foo/bar", ProcedureWithArgs("f"))
	require.Error(t, err)
}
