#!/bin/bash
cd /root
docker save kubeedge/edgemesh-agent:v1.13.1-beta -o edgemesh-agent-v1.13.1-beta.tar
sshpass -p "5210" scp edgemesh-agent-v1.13.1-beta.tar root@node2:~
sshpass -p "5210" scp edgemesh-agent-v1.13.1-beta.tar root@node3:~
