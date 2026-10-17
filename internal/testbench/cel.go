/*
Copyright 2026 operator-testbench contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testbench

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
	"k8s.io/apiserver/pkg/cel/library"
)

// CELEnv compiles the bool expressions a scenario's assert steps carry. The
// live object is bound as `self` and a list's items as `items`, both dynamic,
// because the suite reads every object as unstructured. The Kubernetes CEL
// libraries are on, so an author can write the same quantity(), regex and list
// helpers a CRD validation rule would use.
type CELEnv struct {
	env *cel.Env
}

// NewCELEnv builds the environment once per process; compiling is per expression.
func NewCELEnv() (*CELEnv, error) {
	env, err := cel.NewEnv(
		cel.Variable("self", cel.DynType),
		cel.Variable("items", cel.ListType(cel.DynType)),
		// An int64 from the API server and a literal 3 compare as equal either
		// way, but `<` and `>=` across int and double need this switch.
		cel.CrossTypeNumericComparisons(true),
		library.Lists(),
		library.Regex(),
		library.Quantity(),
		library.Format(),
	)
	if err != nil {
		return nil, fmt.Errorf("cel environment: %w", err)
	}
	return &CELEnv{env: env}, nil
}

// Program is one compiled expression, kept with its source so failures quote it.
type Program struct {
	Expr string
	prg  cel.Program
}

// CompileBool compiles expr and refuses anything that does not type-check to
// bool: a bare field such as `self.status.state` is a mistake, not a truthy test.
func (e *CELEnv) CompileBool(expr string) (*Program, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cel %q: %w", expr, issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("cel %q must evaluate to bool, got %s; compare the value instead", expr, ast.OutputType())
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel %q: %w", expr, err)
	}
	return &Program{Expr: expr, prg: prg}, nil
}

// Check evaluates the program against self (an object's content) or items (a
// list's item contents). Only the binding that is non-nil is declared, so an
// expression that reaches for the other fails with a clear error. A false
// result and an evaluation error (a missing key, for example) are both errors,
// so a polling caller treats them alike: not yet.
func (p *Program) Check(ctx context.Context, self any, items []any) error {
	in := map[string]any{}
	if self != nil {
		in["self"] = self
	}
	if items != nil {
		in["items"] = items
	}
	out, _, err := p.prg.ContextEval(ctx, in)
	if err != nil {
		return fmt.Errorf("cel %q: %w", p.Expr, err)
	}
	ok, isBool := out.Value().(bool)
	if !isBool {
		return fmt.Errorf("cel %q evaluated to %T, want bool", p.Expr, out.Value())
	}
	if !ok {
		return fmt.Errorf("cel %q is false", p.Expr)
	}
	return nil
}
