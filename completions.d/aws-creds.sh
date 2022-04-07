#!/bin/bash

if [ ! -z "$ZSH_NAME" ]; then
    autoload bashcompinit
    bashcompinit
fi

_aws_creds () {
	COMPREPLY=($(
                   ls ~/.aws_creds | grep -v ^_ | grep '.sh$' | sed s/\.sh$// | grep "^${COMP_WORDS[$COMP_CWORD]}"
               ))
}

complete -F _aws_creds aws-creds
