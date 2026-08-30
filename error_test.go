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

package pluginrpc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"pluginrpc.com/pluginrpc"
)

func TestWrapErrorContextCanceled(t *testing.T) {
	t.Parallel()

	err := pluginrpc.WrapError(context.Canceled)
	require.Equal(t, pluginrpc.CodeCanceled, err.Code())
	require.ErrorIs(t, err, context.Canceled)
}

func TestWrapErrorContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	err := pluginrpc.WrapError(context.DeadlineExceeded)
	require.Equal(t, pluginrpc.CodeDeadlineExceeded, err.Code())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestContextErrorRoundTrip(t *testing.T) {
	t.Parallel()

	// context.Canceled must survive a proto round-trip and still satisfy errors.Is.
	canceledRestored := pluginrpc.NewErrorForProto(pluginrpc.WrapError(context.Canceled).ToProto())
	require.Equal(t, pluginrpc.CodeCanceled, canceledRestored.Code())
	require.ErrorIs(t, canceledRestored, context.Canceled)

	// context.DeadlineExceeded must survive a proto round-trip and still satisfy errors.Is.
	deadlineRestored := pluginrpc.NewErrorForProto(pluginrpc.WrapError(context.DeadlineExceeded).ToProto())
	require.Equal(t, pluginrpc.CodeDeadlineExceeded, deadlineRestored.Code())
	require.ErrorIs(t, deadlineRestored, context.DeadlineExceeded)
}

func TestNewErrorContextCanceled(t *testing.T) {
	t.Parallel()

	// A *Error with CodeCanceled satisfies errors.Is(err, context.Canceled)
	// regardless of the underlying message.
	err := pluginrpc.NewError(pluginrpc.CodeCanceled, errors.New("operation was canceled"))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "operation was canceled", err.Unwrap().Error())
}
