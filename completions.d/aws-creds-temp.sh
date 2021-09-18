#!/bin/bash



_aws_creds_tmp () {
	COMPREPLY=($(
                   ls ~/.aws_creds | grep -v ^_ | sed s/\.sh$// | grep "^${COMP_WORDS[$COMP_CWORD]}"
               ))
}

complete -F _aws_creds_tmp aws-creds-temp
