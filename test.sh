#!/bin/bash
set -eou pipefail
make check
make
(cd examples/simple/python; for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/simple/go;     for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/simple/docker; for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/complex;       for name in *; do (cd $name && timeout 600 python test.py); done)
(cd examples/misc;          for name in *; do (cd $name && timeout 600 python test.py); done)
(
    cd lib
    nontest=$(ls *.go | grep -v _test.go)
    for test in $(ls *_test.go | grep -v lib_test.go); do
        go test lib_test.go $nontest $test -o /tmp/libaws.test -c
        timeout 600 /tmp/libaws.test -test.v -test.failfast
    done
)
