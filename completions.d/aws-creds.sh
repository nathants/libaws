#!/bin/bash



_aws_creds () {
	COMPREPLY=($(
                   ls ~/.aws_creds | grep -v ^_ | sed s/\.sh$// | grep "^${COMP_WORDS[$COMP_CWORD]}"
               ))
}

complete -F _aws_creds aws-creds
