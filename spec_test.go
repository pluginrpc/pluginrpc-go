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

func TestMergeSpecsSuccess(t *testing.T) {
	t.Parallel()

	procedure1, err := NewProcedure("/foo/bar")
	require.NoError(t, err)
	procedure2, err := NewProcedure("/foo/baz")
	require.NoError(t, err)
	spec1, err := NewSpec(procedure1)
	require.NoError(t, err)
	spec2, err := NewSpec(procedure2)
	require.NoError(t, err)
	spec, err := MergeSpecs(spec1, spec2)
	require.NoError(t, err)
	require.Equal(
		t,
		[]Procedure{procedure1, procedure2},
		spec.Procedures(),
	)
}

func TestMergeSpecsErrorOverlappingPaths(t *testing.T) {
	t.Parallel()

	procedure1, err := NewProcedure("/foo/bar")
	require.NoError(t, err)
	procedure2, err := NewProcedure("/foo/bar")
	require.NoError(t, err)
	spec1, err := NewSpec(procedure1)
	require.NoError(t, err)
	spec2, err := NewSpec(procedure2)
	require.NoError(t, err)
	_, err = MergeSpecs(spec1, spec2)
	require.Error(t, err)
}

func TestMergeSpecsErrorOverlappingArgs(t *testing.T) {
	t.Parallel()

	procedure1, err := NewProcedure("/foo/bar", ProcedureWithArgs("foo", "bar"))
	require.NoError(t, err)
	procedure2, err := NewProcedure("/foo/baz", ProcedureWithArgs("foo", "bar"))
	require.NoError(t, err)
	spec1, err := NewSpec(procedure1)
	require.NoError(t, err)
	spec2, err := NewSpec(procedure2)
	require.NoError(t, err)
	_, err = MergeSpecs(spec1, spec2)
	require.Error(t, err)
}
