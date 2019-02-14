#!/bin/bash

-complete-NAME () {
    if [ $COMP_CWORD = 1 ]; then
        COMPREPLY=($(aws-ec2-ls "${COMP_WORDS[$COMP_CWORD]:-}*" | awk '{print $1}' | LC_ALL=C sort | tr -d ' '))
    fi
}

complete -o dirnames -F -complete-NAME NAME
