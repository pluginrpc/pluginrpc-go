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

package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
	"pluginrpc.com/pluginrpc"
)

const (
	contextPackage   = protogen.GoImportPath("context")
	fmtPackage       = protogen.GoImportPath("fmt")
	pluginrpcPackage = protogen.GoImportPath("pluginrpc.com/pluginrpc")

	generatedFilenameExtension = ".pluginrpc.go"
	generatedPackageSuffix     = "pluginrpc"

	usage = "Flags:\n  -h, --help\tPrint this help and exit.\n      --version\tPrint the version and exit."

	optionStreamingKey         = "streaming"
	optionStreamingValueError  = "error"
	optionStreamingValueWarn   = "warn"
	optionStreamingValueIgnore = "ignore"

	commentWidth = 97 // leave room for "// "

	// To propagate top-level comments, we need the field number of the syntax
	// declaration and the package name in the file descriptor.
	protoSyntaxFieldNum  = 12
	protoPackageFieldNum = 2
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Fprintln(os.Stdout, pluginrpc.Version)
		os.Exit(0)
	}
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Fprintln(os.Stdout, usage)
		os.Exit(0)
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	flags := newFlags()
	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(
		func(plugin *protogen.Plugin) error {
			plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
			if err := validate(plugin, flags); err != nil {
				return err
			}
			return generate(plugin)
		},
	)
}

type flags struct {
	streaming string
}

func newFlags() *flags {
	return &flags{}
}

func (f *flags) Set(name string, value string) error {
	switch name {
	case optionStreamingKey:
		switch value {
		case optionStreamingValueError, optionStreamingValueWarn, optionStreamingValueIgnore:
			f.streaming = value
			return nil
		default:
			return fmt.Errorf("unknown value for parameter %q: %q", name, value)
		}
	default:
		return fmt.Errorf("unknown parameter: %q", name)
	}
}

func validate(plugin *protogen.Plugin, flags *flags) error {
	var streamingError bool
	switch flags.streaming {
	case optionStreamingValueError:
		streamingError = true
	case "", optionStreamingValueWarn:
	case optionStreamingValueIgnore:
		// Ignore, no validation to do at this time since we only validate streaming.
		return nil
	default:
		// This should never happen.
		return fmt.Errorf("unknown value for parameter %q after parsing: %q", optionStreamingKey, flags.streaming)
	}

	var streamingMethods []*protogen.Method
	for _, file := range plugin.Files {
		if file.Generate {
			streamingMethods = append(streamingMethods, getStreamingMethodsForFile(file)...)
		}
	}
	if len(streamingMethods) == 0 {
		return nil
	}
	streamingMethodStrings := make([]string, len(streamingMethods))
	for i, streamingMethod := range streamingMethods {
		streamingMethodStrings[i] = string(streamingMethod.Desc.FullName())
	}
	if streamingError {
		// optionStreamingValueError
		return fmt.Errorf("streaming methods are not supported: %s", strings.Join(streamingMethodStrings, ", "))
	}

	// We're now in optionStreamingValueWarn territory.
	for i, streamingMethodString := range streamingMethodStrings {
		streamingMethodStrings[i] = "  - " + streamingMethodString
	}
	_, err := fmt.Fprintf(
		os.Stderr,
		`Warning: streaming methods are not supported, these methods will be skipped and not part of generated interfaces:

%s

To error on streaming methods, set the parameter "%s=%s".
`,
		strings.Join(streamingMethodStrings, "\n"),
		optionStreamingKey,
		optionStreamingValueError,
	)
	return err
}

func generate(plugin *protogen.Plugin) error {
	for _, file := range plugin.Files {
		if file.Generate {
			if err := generateFile(plugin, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func generateFile(plugin *protogen.Plugin, file *protogen.File) error {
	if len(getUnaryMethodsForFile(file)) == 0 {
		return nil
	}

	file.GoPackageName += generatedPackageSuffix

	generatedFilenamePrefixToSlash := filepath.ToSlash(file.GeneratedFilenamePrefix)
	file.GeneratedFilenamePrefix = path.Join(
		path.Dir(generatedFilenamePrefixToSlash),
		string(file.GoPackageName),
		path.Base(generatedFilenamePrefixToSlash),
	)
	generatedFile := plugin.NewGeneratedFile(
		file.GeneratedFilenamePrefix+generatedFilenameExtension,
		protogen.GoImportPath(path.Join(
			string(file.GoImportPath),
			string(file.GoPackageName),
		)),
	)
	generatedFile.Import(file.GoImportPath)

	generatePreamble(generatedFile, file)
	generatePathConstants(generatedFile, file)
	for _, service := range file.Services {
		names := newNames(service)
		generateSpecBuilder(generatedFile, service, names)
		generateClientInterface(generatedFile, service, names)
		generateClientConstructor(generatedFile, service, names)
		generateHandlerInterface(generatedFile, service, names)
		generateServerInterface(generatedFile, service, names)
		generateServerConstructor(generatedFile, service, names)
		generateServerRegister(generatedFile, service, names)
	}
	generatedFile.P("// *** PRIVATE ***")
	generatedFile.P()
	for _, service := range file.Services {
		names := newNames(service)
		generateClientImplementation(generatedFile, service, names)
		generateServerImplementation(generatedFile, service, names)
	}
	return nil
}

func generatePreamble(g *protogen.GeneratedFile, file *protogen.File) {
	syntaxPath := protoreflect.SourcePath{protoSyntaxFieldNum}
	syntaxLocation := file.Desc.SourceLocations().ByPath(syntaxPath)
	for _, comment := range syntaxLocation.LeadingDetachedComments {
		leadingComments(g, protogen.Comments(comment), false /* deprecated */)
	}
	g.P()
	leadingComments(g, protogen.Comments(syntaxLocation.LeadingComments), false /* deprecated */)
	g.P()

	programName := filepath.Base(os.Args[0])
	// Remove .exe suffix on Windows so that generated code is stable, regardless
	// of whether it was generated on a Windows machine or not.
	if ext := filepath.Ext(programName); strings.ToLower(ext) == ".exe" {
		programName = strings.TrimSuffix(programName, ext)
	}
	g.P("// Code generated by ", programName, ". DO NOT EDIT.")
	g.P("//")
	if file.Proto.GetOptions().GetDeprecated() {
		wrapComments(g, file.Desc.Path(), " is a deprecated file.")
	} else {
		g.P("// Source: ", file.Desc.Path())
	}
	g.P()

	pkgPath := protoreflect.SourcePath{protoPackageFieldNum}
	pkgLocation := file.Desc.SourceLocations().ByPath(pkgPath)
	for _, comment := range pkgLocation.LeadingDetachedComments {
		leadingComments(g, protogen.Comments(comment), false /* deprecated */)
	}
	g.P()
	leadingComments(g, protogen.Comments(pkgLocation.LeadingComments), false /* deprecated */)

	g.P("package ", file.GoPackageName)
	g.P()
	wrapComments(g, "This is a compile-time assertion to ensure that this generated file ",
		"and the pluginrpc package are compatible. If you get a compiler error that this constant ",
		"is not defined, this code was generated with a version of pluginrpc newer than the one ",
		"compiled into your binary. You can fix the problem by either regenerating this code ",
		"with an older version of pluginrpc or updating the pluginrpc version compiled into your binary.")
	g.P("const _ = ", pluginrpcPackage.Ident("IsAtLeastVersion0_1_0"))
	g.P()
}

func generatePathConstants(g *protogen.GeneratedFile, file *protogen.File) {
	unaryMethods := getUnaryMethodsForFile(file)
	if len(unaryMethods) == 0 {
		return
	}
	g.P("const (")
	for _, method := range unaryMethods {
		wrapComments(g, pathConstName(method), " is the path of the ",
			method.Parent.Desc.Name(), "'s ", method.Desc.Name(), " RPC.")
		g.P(pathConstName(method), ` = "`, fmt.Sprintf("/%s/%s", method.Parent.Desc.FullName(), method.Desc.Name()), `"`)
	}
	g.P(")")
	g.P()
}

func generateSpecBuilder(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.SpecBuilder, " builds a Spec for the ", service.Desc.FullName(), " service.")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.AnnotateSymbol(names.SpecBuilder, protogen.Annotation{Location: service.Location})
	g.P("type ", names.SpecBuilder, " struct {")
	for _, method := range unaryMethods {
		g.P(method.GoName, " []", pluginrpcPackage.Ident("ProcedureOption"))
	}
	g.P("}")
	g.P()
	wrapComments(g, "Build builds a Spec for the ", service.Desc.FullName(), " service.")
	g.P("func (s ", names.SpecBuilder, ") Build() (", pluginrpcPackage.Ident("Spec"), ", error) {")
	g.P("procedures := make([]", pluginrpcPackage.Ident("Procedure"), ", 0, ", len(unaryMethods), ")")
	for i, method := range unaryMethods {
		equals := "="
		if i == 0 {
			equals = ":="
		}
		g.P("procedure, err ", equals, " ", pluginrpcPackage.Ident("NewProcedure"), "(", pathConstName(method), ", s.", method.GoName, "...)")
		g.P("if err != nil {")
		g.P("return nil, err")
		g.P("}")
		g.P("procedures = append(procedures, procedure)")
	}
	g.P("return ", pluginrpcPackage.Ident("NewSpec"), "(procedures)")
	g.P("}")
	g.P()
}
func generateClientInterface(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.Client, " is a client for the ", service.Desc.FullName(), " service.")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.AnnotateSymbol(names.Client, protogen.Annotation{Location: service.Location})
	g.P("type ", names.Client, " interface {")
	for _, method := range unaryMethods {
		g.AnnotateSymbol(names.Client+"."+method.GoName, protogen.Annotation{Location: method.Location})
		leadingComments(
			g,
			method.Comments.Leading,
			isDeprecatedMethod(method),
		)
		g.P(clientSignature(g, method, false /* named */))
	}
	g.P("}")
	g.P()
}

func generateClientConstructor(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	// Client constructor.
	wrapComments(g, names.ClientConstructor, " constructs a client for the ", service.Desc.FullName(), " service.")
	g.P("//")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.ClientConstructor, " (client ", pluginrpcPackage.Ident("Client"),
		") (", names.Client, ", error) {")
	g.P("return &", names.ClientImpl, "{")
	g.P("client: client,")
	g.P("}, nil")
	g.P("}")
	g.P()
}

func generateClientImplementation(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	// Client struct.
	wrapComments(g, names.ClientImpl, " implements ", names.Client, ".")
	g.P("type ", names.ClientImpl, " struct {")
	g.P("client ", pluginrpcPackage.Ident("Client"))
	g.P("}")
	g.P()
	for _, method := range unaryMethods {
		generateClientMethod(g, method, names)
	}
}

func generateClientMethod(g *protogen.GeneratedFile, method *protogen.Method, names names) {
	receiver := names.ClientImpl
	wrapComments(g, method.GoName, " calls ", method.Desc.FullName(), ".")
	if isDeprecatedMethod(method) {
		g.P("//")
		deprecated(g)
	}
	g.P("func (c *", receiver, ") ", clientSignature(g, method, true /* named */), " {")
	g.P("res := &", g.QualifiedGoIdent(method.Output.GoIdent), "{}")
	g.P("if err := c.client.Call(ctx, ", pathConstName(method), ", req, res, opts...); err != nil {")
	g.P("return nil, err")
	g.P("}")
	g.P("return res, nil")
	g.P("}")
	g.P()
}

func generateHandlerInterface(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.Handler, " is an implementation of the ", service.Desc.FullName(), " service.")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.AnnotateSymbol(names.Handler, protogen.Annotation{Location: service.Location})
	g.P("type ", names.Handler, " interface {")
	for _, method := range unaryMethods {
		leadingComments(
			g,
			method.Comments.Leading,
			isDeprecatedMethod(method),
		)
		g.AnnotateSymbol(names.Handler+"."+method.GoName, protogen.Annotation{Location: method.Location})
		g.P(handlerSignature(g, method))
	}
	g.P("}")
	g.P()
}

func generateServerInterface(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.Server, " serves the ", service.Desc.FullName(), " service.")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.AnnotateSymbol(names.Server, protogen.Annotation{Location: service.Location})
	g.P("type ", names.Server, " interface {")
	for _, method := range unaryMethods {
		leadingComments(
			g,
			method.Comments.Leading,
			isDeprecatedMethod(method),
		)
		g.AnnotateSymbol(names.Handler+"."+method.GoName, protogen.Annotation{Location: method.Location})
		g.P(serverSignature(g, method, false))
	}
	g.P("}")
	g.P()
}

func generateServerConstructor(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.ServerConstructor, " constructs a server for the ", service.Desc.FullName(), " service.")
	g.P("//")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.ServerConstructor, " (handler ", pluginrpcPackage.Ident("Handler"),
		", ", unexport(names.Handler), " ", names.Handler, ") ", names.Server, " {")
	g.P("return &", names.ServerImpl, "{")
	g.P("handler: handler,")
	g.P(unexport(names.Handler), ": ", unexport(names.Handler), ",")
	g.P("}")
	g.P("}")
	g.P()
}

func generateServerRegister(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.ServerRegister, " registers the server for the ", service.Desc.FullName(), " service.")
	g.P("//")
	if isDeprecatedService(service) {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.ServerRegister, " (serverRegistrar ", pluginrpcPackage.Ident("ServerRegistrar"),
		", ", unexport(names.Server), " ", names.Server, ") {")
	for _, method := range unaryMethods {
		g.P("serverRegistrar.Register(", pathConstName(method), ", ", unexport(names.Server), ".", method.GoName, ")")
	}
	g.P("}")
	g.P()
}

func generateServerImplementation(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	unaryMethods := getUnaryMethodsForService(service)
	if len(unaryMethods) == 0 {
		return
	}
	wrapComments(g, names.ServerImpl, " implements ", names.Server, ".")
	g.P("type ", names.ServerImpl, " struct {")
	g.P("handler ", pluginrpcPackage.Ident("Handler"))
	g.P(unexport(names.Handler), " ", names.Handler)
	g.P("}")
	g.P()
	for _, method := range unaryMethods {
		generateServerMethod(g, method, names)
	}
}

func generateServerMethod(g *protogen.GeneratedFile, method *protogen.Method, names names) {
	receiver := names.ServerImpl
	wrapComments(g, method.GoName, " calls ", method.Desc.FullName(), ".")
	if isDeprecatedMethod(method) {
		g.P("//")
		deprecated(g)
	}
	g.P("func (c *", receiver, ") ", serverSignature(g, method, true /* named */), " {")
	g.P("return c.handler.Handle(")
	g.P("ctx,")
	g.P("handleEnv,")
	g.P("&", g.QualifiedGoIdent(method.Input.GoIdent), "{},")
	g.P("func(ctx ", contextPackage.Ident("Context"), ", anyReq any) (any, error) {")
	g.P("req, ok := anyReq.(*", g.QualifiedGoIdent(method.Input.GoIdent), ")")
	g.P("if !ok {")
	g.P("return nil, ", fmtPackage.Ident("Errorf"), `("could not cast %T to a *`, g.QualifiedGoIdent(method.Input.GoIdent), `", anyReq)`)
	g.P("}")
	g.P("return c.", unexport(names.Handler), ".", method.GoName, "(ctx, req)")
	g.P("},")
	g.P("options...,")
	g.P(")")
	g.P("}")
	g.P()
}

func clientSignature(g *protogen.GeneratedFile, method *protogen.Method, named bool) string {
	// unary; symmetric so we can re-use server templating
	return method.GoName + clientSignatureParams(g, method, named)
}

func clientSignatureParams(g *protogen.GeneratedFile, method *protogen.Method, named bool) string {
	ctxName := "ctx "
	reqName := "req "
	optsName := "opts "
	if !named {
		ctxName, reqName, optsName = "", "", ""
	}
	// unary
	return "(" + ctxName + g.QualifiedGoIdent(contextPackage.Ident("Context")) +
		", " + reqName + "*" + g.QualifiedGoIdent(method.Input.GoIdent) +
		", " + optsName + "..." + g.QualifiedGoIdent(pluginrpcPackage.Ident("CallOption")) + ") " +
		"(*" + g.QualifiedGoIdent(method.Output.GoIdent) + ", error)"
}

func handlerSignature(g *protogen.GeneratedFile, method *protogen.Method) string {
	return method.GoName + handlerSignatureParams(g, method, false)
}

func handlerSignatureParams(g *protogen.GeneratedFile, method *protogen.Method, named bool) string {
	ctxName := "ctx "
	reqName := "req "
	if !named {
		ctxName, reqName = "", ""
	}
	// unary
	return "(" + ctxName + g.QualifiedGoIdent(contextPackage.Ident("Context")) +
		", " + reqName + "*" + g.QualifiedGoIdent(method.Input.GoIdent) + ") " +
		"(*" + g.QualifiedGoIdent(method.Output.GoIdent) + ", error)"
}

func serverSignature(g *protogen.GeneratedFile, method *protogen.Method, named bool) string {
	return method.GoName + serverSignatureParams(g, method, named)
}

func serverSignatureParams(g *protogen.GeneratedFile, _ *protogen.Method, named bool) string {
	ctxName := "ctx "
	handleEnvName := "handleEnv "
	optionsName := "options"
	if !named {
		ctxName, handleEnvName, optionsName = "", "", ""
	}
	// unary
	return "(" + ctxName + g.QualifiedGoIdent(contextPackage.Ident("Context")) +
		", " + handleEnvName + g.QualifiedGoIdent(pluginrpcPackage.Ident("HandleEnv")) +
		", " + optionsName + " ..." + g.QualifiedGoIdent(pluginrpcPackage.Ident("HandleOption")) +
		") error"
}

func pathConstName(m *protogen.Method) string {
	return fmt.Sprintf("%s%sPath", m.Parent.GoName, m.GoName)
}

func isDeprecatedService(service *protogen.Service) bool {
	serviceOptions, ok := service.Desc.Options().(*descriptorpb.ServiceOptions)
	return ok && serviceOptions.GetDeprecated()
}

func isDeprecatedMethod(method *protogen.Method) bool {
	methodOptions, ok := method.Desc.Options().(*descriptorpb.MethodOptions)
	return ok && methodOptions.GetDeprecated()
}

func getUnaryMethodsForFile(file *protogen.File) []*protogen.Method {
	var methods []*protogen.Method
	for _, service := range file.Services {
		methods = append(methods, getUnaryMethodsForService(service)...)
	}
	return methods
}

func getUnaryMethodsForService(service *protogen.Service) []*protogen.Method {
	var methods []*protogen.Method
	for _, method := range service.Methods {
		if isUnaryMethod(method) {
			methods = append(methods, method)
		}
	}
	return methods
}

func getStreamingMethodsForFile(file *protogen.File) []*protogen.Method {
	var methods []*protogen.Method
	for _, service := range file.Services {
		methods = append(methods, getStreamingMethodsForService(service)...)
	}
	return methods
}

func getStreamingMethodsForService(service *protogen.Service) []*protogen.Method {
	var methods []*protogen.Method
	for _, method := range service.Methods {
		if !isUnaryMethod(method) {
			methods = append(methods, method)
		}
	}
	return methods
}

func isUnaryMethod(method *protogen.Method) bool {
	return !(method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer())
}

// Raggedy comments in the generated code are driving me insane. This
// word-wrapping function is ruinously inefficient, but it gets the job done.
func wrapComments(g *protogen.GeneratedFile, elems ...any) {
	text := &bytes.Buffer{}
	for _, el := range elems {
		switch el := el.(type) {
		case protogen.GoIdent:
			fmt.Fprint(text, g.QualifiedGoIdent(el))
		default:
			fmt.Fprint(text, el)
		}
	}
	words := strings.Fields(text.String())
	text.Reset()
	var pos int
	for _, word := range words {
		numRunes := utf8.RuneCountInString(word)
		if pos > 0 && pos+numRunes+1 > commentWidth {
			g.P("// ", text.String())
			text.Reset()
			pos = 0
		}
		if pos > 0 {
			text.WriteRune(' ')
			pos++
		}
		text.WriteString(word)
		pos += numRunes
	}
	if text.Len() > 0 {
		g.P("// ", text.String())
	}
}

func leadingComments(g *protogen.GeneratedFile, comments protogen.Comments, isDeprecated bool) {
	if comments.String() != "" {
		g.P(strings.TrimSpace(comments.String()))
	}
	if isDeprecated {
		if comments.String() != "" {
			g.P("//")
		}
		deprecated(g)
	}
}

func deprecated(g *protogen.GeneratedFile) {
	g.P("// Deprecated: do not use.")
}

func unexport(s string) string {
	lowercased := strings.ToLower(s[:1]) + s[1:]
	switch lowercased {
	// https://go.dev/ref/spec#Keywords
	case "break", "default", "func", "interface", "select",
		"case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch",
		"const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var":
		return "_" + lowercased
	default:
		return lowercased
	}
}

type names struct {
	Base              string
	SpecBuilder       string
	Client            string
	ClientConstructor string
	ClientImpl        string
	Handler           string
	Server            string
	ServerConstructor string
	ServerRegister    string
	ServerImpl        string
}

func newNames(service *protogen.Service) names {
	base := service.GoName
	return names{
		Base:              base,
		SpecBuilder:       base + "SpecBuilder",
		Client:            base + "Client",
		ClientConstructor: "New" + base + "Client",
		ClientImpl:        unexport(base) + "Client",
		Handler:           base + "Handler",
		Server:            base + "Server",
		ServerConstructor: "New" + base + "Server",
		ServerRegister:    "Register" + base + "Server",
		ServerImpl:        unexport(base) + "Server",
	}
}
