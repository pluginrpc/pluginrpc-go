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

// Package main implements a client that calls the EchoError RPC on the
// example-plugin plugin.
//
// This will parse the first arg as an error Code, and all further args will
// comprise the error message.
package main

import (
	"context"
	"os"
	"strconv"
	"strings"

	pluginrpcv1 "buf.build/gen/go/pluginrpc/pluginrpc/protocolbuffers/go/pluginrpc/v1"
	"pluginrpc.com/pluginrpc"
	examplev1 "pluginrpc.com/pluginrpc/internal/example/gen/pluginrpc/example/v1"
	"pluginrpc.com/pluginrpc/internal/example/gen/pluginrpc/example/v1/examplev1pluginrpc"
)

func main() {
	if err := run(); err != nil {
		if errString := err.Error(); errString != "" {
			_, _ = os.Stderr.Write([]byte(errString + "\n"))
		}
		os.Exit(pluginrpc.WrapExitError(err).ExitCode())
	}
}

func run() error {
	client := pluginrpc.NewClient(pluginrpc.NewExecRunner("example-plugin"))
	echoServiceClient, err := examplev1pluginrpc.NewEchoServiceClient(client)
	if err != nil {
		return err
	}
	code, err := strconv.ParseInt(os.Args[1], 10, 32)
	if err != nil {
		return err
	}
	_, err = echoServiceClient.EchoError(
		context.Background(),
		&examplev1.EchoErrorRequest{
			Code:    pluginrpcv1.Code(code),
			Message: strings.Join(os.Args[2:], " "),
		},
	)
	return err
}
