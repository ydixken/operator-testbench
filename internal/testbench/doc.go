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

// Package testbench is the engine behind the e2e suite: it loads operator
// packs and their YAML scenarios, validates them, installs the operator under
// test, and runs each step against a cluster. It is written at Gomega level
// and never imports Ginkgo, so every piece has a unit test without a cluster;
// test/e2e turns the loaded scenarios into the Ginkgo spec tree.
package testbench
