#!/bin/bash

if [ ! -z "$ZSH_NAME" ]; then
    autoload bashcompinit
    bashcompinit
fi

COMP_WORDBREAKS=" '><;|&(:" # removed = so the we can complete cloudwatch dimensions like InstanceId=i-0123...

_libaws () {

    if [ "${COMP_CWORD}" = 1 ] && [ -z "${COMP_WORDS[1]}" ]; then
        COMPREPLY=($(libaws -h | awk '{print $1}'))
    elif   [ ${COMP_WORDS[1]} = s3-ls          ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif   [ ${COMP_WORDS[1]} = s3-ls-versions ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-get           ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-put           ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-presign-get   ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-presign-put   ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-head          ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-rm            ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-rm-versions   ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg"))
    elif [ ${COMP_WORDS[1]} = s3-rm-bucket     ] && [ $COMP_CWORD = 2 ]; then arg=""; for i in $(seq 2 $COMP_CWORD); do arg="${arg}${COMP_WORDS[$i]}"; done; COMPREPLY=($(libaws s3-ls -q "$arg" 2>/dev/null | grep "^$arg" | tr -d /))

    elif [ $COMP_CWORD = 1 ]; then
        if [ -z "${COMP_WORDS[1]}" ]; then
            COMPREPLY=($(libaws -h | awk '{print $1}'))
        else
	        COMPREPLY=($(libaws -h 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[$COMP_CWORD]}"))
        fi

    elif [ $COMP_CWORD = 2 ]; then

        if   [ ${COMP_WORDS[1]} = ec2-ls          ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ssh-user    ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-new-ami     ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-rm          ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | grep -e running -e stopped | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-reboot      ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-stop        ]; then COMPREPLY=($(libaws ec2-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ssh         ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ip          ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-ip-private  ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-dns         ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-dns-private ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ec2-wait-ssh    ]; then COMPREPLY=($(libaws ec2-ls -s running 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = creds-set    ]; then COMPREPLY=($(libaws creds-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = dynamodb-rm ];        then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-scan ]; then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-rm   ]; then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-rm-all   ]; then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-put  ]; then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-get  ]; then COMPREPLY=($(libaws dynamodb-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = infra-parse ];  then COMPREPLY=($(find . -type f 2>/dev/null | grep -E -e '\.yml$' -e '\.yaml$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = infra-ensure ]; then COMPREPLY=($(find . -type f 2>/dev/null | grep -E -e '\.yml$' -e '\.yaml$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = infra-api    ]; then COMPREPLY=($(find . -type f 2>/dev/null | grep -E -e '\.yml$' -e '\.yaml$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = infra-rm ];     then COMPREPLY=($(find . -type f 2>/dev/null | grep -E -e '\.yml$' -e '\.yaml$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = infra-url ];     then COMPREPLY=($(find . -type f 2>/dev/null | grep -E -e '\.yml$' -e '\.yaml$'  | sed s:./:: | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = iam-rm-role     ]; then COMPREPLY=($(libaws iam-ls-roles 2>/dev/null | jq -r .RoleName | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = lambda-rm     ]; then COMPREPLY=($(libaws lambda-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = lambda-describe     ]; then COMPREPLY=($(libaws lambda-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = lambda-arn    ]; then COMPREPLY=($(libaws lambda-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = lambda-vars   ]; then COMPREPLY=($(libaws lambda-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))


        elif [ ${COMP_WORDS[1]} = sqs-stats ]; then COMPREPLY=($(libaws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-purge ]; then COMPREPLY=($(libaws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = sqs-rm    ]; then COMPREPLY=($(libaws sqs-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = logs-search ]; then COMPREPLY=($(libaws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = logs-near   ]; then COMPREPLY=($(libaws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = logs-tail   ]; then COMPREPLY=($(libaws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = logs-rm     ]; then COMPREPLY=($(libaws logs-ls 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = ecr-ls-tags ]; then COMPREPLY=($(libaws ecr-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = ecr-rm      ]; then COMPREPLY=($(libaws ecr-ls 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        elif [ ${COMP_WORDS[1]} = cloudwatch-ls-metrics ];    then COMPREPLY=($(libaws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-ls-dimensions ]; then COMPREPLY=($(libaws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-get-metric ];    then COMPREPLY=($(libaws cloudwatch-ls-namespaces 2>/dev/null | grep "^${COMP_WORDS[2]}"))

        fi

    elif [ $COMP_CWORD = 3 ]; then

        if   [ ${COMP_WORDS[1]} = cloudwatch-ls-dimensions ]; then COMPREPLY=($(libaws cloudwatch-ls-metrics "${COMP_WORDS[2]}" 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[3]}"))
        elif [ ${COMP_WORDS[1]} = cloudwatch-get-metric ];    then COMPREPLY=($(libaws cloudwatch-ls-metrics "${COMP_WORDS[2]}" 2>/dev/null | awk '{print $1}' | grep "^${COMP_WORDS[3]}"))
        elif [ ${COMP_WORDS[1]} = dynamodb-item-get ];    then COMPREPLY=($(libaws dynamodb-item-scan "${COMP_WORDS[2]}" 2>/dev/null | jq -r .id | grep "^${COMP_WORDS[3]}" | sed s/^/id:s:/))
        fi

    elif [ $COMP_CWORD = 4 ]; then
        if [ ${COMP_WORDS[1]} = cloudwatch-get-metric ]; then COMPREPLY=($(libaws cloudwatch-ls-dimensions "${COMP_WORDS[2]}" $(echo "${COMP_WORDS[3]}" | cut -d, -f1) 2>/dev/null | grep "^${COMP_WORDS[4]}"))
        fi

    fi

}

complete -F _libaws libaws
