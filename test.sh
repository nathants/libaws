#!/bin/bash
set -xeou pipefail
make check
make
(cd examples/simple/python; for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/simple/go;     for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/simple/docker; for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/complex;       for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/_misc;         for name in *; do (cd $name && timeout 600 python test.py); done)
(cd lib; nontest=$(ls *.go | grep -v _test.go); for test in $(ls *_test.go | grep -v lib_test.go); do timeout 600 go test -failfast -v lib_test.go $nontest $test; done)
