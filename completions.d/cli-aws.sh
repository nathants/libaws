#!/bin/bash

if [ ! -z "$ZSH_NAME" ]; then
    autoload bashcompinit
    bashcompinit
fi

_cli_aws () {
    if [ $COMP_CWORD = 1 ]; then
	    COMPREPLY=($(cli-aws -h 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[$COMP_CWORD]}"))
    elif [ $COMP_CWORD = 2 ]; then

        if   [ ${COMP_WORDS[1]} = ec2-ls  ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-rm  ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ssh ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = sqs-stats ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-purge ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-rm    ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = logs-search ]; then COMPREPLY=($(cli-aws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = logs-near   ]; then COMPREPLY=($(cli-aws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = ecr-ls-tags ]; then COMPREPLY=($(cli-aws ecr-ls-images 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        fi
    fi
}

complete -F _cli_aws cli-aws
