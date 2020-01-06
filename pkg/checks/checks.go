/*
   Copyright 2018 Cruise LLC

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

// Package testutil collects commonly use check functions.
package checks

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func Err(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func DeepEqual(t *testing.T, expected, actual interface{}, format string, args ...interface{}) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		args = append(args, spew.Sdump(expected), spew.Sdump(actual))
		t.Fatalf(format+"\nExpected:\n\t%#s != \nActual:\n\t%#s", args...)
	}
}
