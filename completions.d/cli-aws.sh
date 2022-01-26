#!/bin/bash

if [ ! -z "$ZSH_NAME" ]; then
    autoload bashcompinit
    bashcompinit
fi

COMP_WORDBREAKS=" '><;|&(:" # removed = so the we can complete cloudwatch dimensions like InstanceId=i-0123...

_cli_aws () {

    if [ ${COMP_WORDS[1]} = s3-ls ]; then
        arg=""
        for i in $(seq 2 $COMP_CWORD); do
            arg="${arg}${COMP_WORDS[$i]}"
        done
        COMPREPLY=($(cli-aws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))

    elif [ $COMP_CWORD = 1 ]; then

	    COMPREPLY=($(cli-aws -h 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[$COMP_CWORD]}"))

    elif [ $COMP_CWORD = 2 ]; then

        if   [ ${COMP_WORDS[1]} = ec2-ls          ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-rm          ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-stop        ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ssh         ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ip          ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ip-private  ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-dns         ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-dns-private ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-wait-ssh    ]; then COMPREPLY=($(cli-aws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif   [ ${COMP_WORDS[1]} = dynamodb-scan ]; then COMPREPLY=($(cli-aws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = lambda-ensure    ]; then COMPREPLY=($(find -type f 2>/dev/null | grep -Ev -e '/\.' -e '\.pyc$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = lambda-rm    ]; then COMPREPLY=($(find -type f 2>/dev/null | grep -Ev -e '/\.' -e '\.pyc$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = sqs-stats ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-purge ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-rm    ]; then COMPREPLY=($(cli-aws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = logs-search ]; then COMPREPLY=($(cli-aws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = logs-near   ]; then COMPREPLY=($(cli-aws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = ecr-ls-tags ]; then COMPREPLY=($(cli-aws ecr-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ecr-rm      ]; then COMPREPLY=($(cli-aws ecr-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = cloudwatch-ls-metrics ];    then COMPREPLY=($(cli-aws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-ls-dimensions ]; then COMPREPLY=($(cli-aws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-get-metric ];    then COMPREPLY=($(cli-aws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        fi

    elif [ $COMP_CWORD = 3 ]; then

        if   [ ${COMP_WORDS[1]} = cloudwatch-ls-dimensions ]; then COMPREPLY=($(cli-aws cloudwatch-ls-metrics "${COMP_WORDS[2]}" 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[3]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-get-metric ];    then COMPREPLY=($(cli-aws cloudwatch-ls-metrics "${COMP_WORDS[2]}" 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[3]}"))
        fi

    elif [ $COMP_CWORD = 4 ]; then
        if [ ${COMP_WORDS[1]} = cloudwatch-get-metric ]; then COMPREPLY=($(cli-aws cloudwatch-ls-dimensions "${COMP_WORDS[2]}" $(echo "${COMP_WORDS[3]}" | cut -d, -f1) 2>/dev/null | grep "^${COMP_WORDS[4]}"))
        fi

    fi

}

complete -F _cli_aws cli-aws
