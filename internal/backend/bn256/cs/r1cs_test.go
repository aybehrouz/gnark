// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by gnark DO NOT EDIT

package cs_test

import (
	"bytes"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/internal/backend/circuits"
	"github.com/consensys/gurvy"
	"reflect"
	"testing"

	bn256backend "github.com/consensys/gnark/internal/backend/bn256/cs"
)

func TestSerialization(t *testing.T) {
	var buffer bytes.Buffer
	for name, circuit := range circuits.Circuits {
		buffer.Reset()

		r1cs, err := frontend.Compile(gurvy.BN256, backend.GROTH16, circuit.Circuit)
		if err != nil {
			t.Fatal(err)
		}
		if testing.Short() && r1cs.GetNbConstraints() > 50 {
			continue
		}

		r1cs.SetLoggerOutput(nil) // no need to serialize.

		{
			t.Log(name)
			var err error
			var written, read int64
			written, err = r1cs.WriteTo(&buffer)
			if err != nil {
				t.Fatal(err)
			}
			var reconstructed bn256backend.R1CS
			read, err = reconstructed.ReadFrom(&buffer)
			if err != nil {
				t.Fatal(err)
			}
			if written != read {
				t.Fatal("didn't read same number of bytes we wrote")
			}
			// compare both
			if !reflect.DeepEqual(r1cs, &reconstructed) {
				t.Fatal("round trip serialization failed")
			}
		}
	}
}
